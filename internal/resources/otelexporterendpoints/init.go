/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package otelexporterendpoints

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	collectionutils "github.com/formancehq/go-libs/v5/pkg/types/collections"
	"github.com/formancehq/go-libs/v5/pkg/types/pointer"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

//+kubebuilder:rbac:groups=formance.com,resources=otelexporterendpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=otelexporterendpoints/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=otelexporterendpoints/finalizers,verbs=update

const (
	deploymentName  = "otel-collector"
	serviceName     = "otel-collector"
	collectorPort   = 4318
	healthCheckPort = 13133

	collectorFinalizer = "otelexporterendpoint.formance.com/finalizer"

	managedByLabel = CollectorManagedByLabel
	managedByValue = CollectorManagedByValue

	collectorSignalAnnotation = "formance.com/otel-collector-signals"
)

func collectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": deploymentName,
		managedByLabel:           managedByValue,
	}
}

func Reconcile(ctx Context, endpoint *v1beta1.OtelExporterEndpoint) error {
	selector, err := selectorFromSpec(endpoint.Spec.StackSelector)
	if err != nil {
		return err
	}

	var stacks v1beta1.StackList
	if err := ctx.GetClient().List(ctx, &stacks, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return err
	}

	logger := log.FromContext(ctx)
	targetedStacks := map[string]bool{}
	stackNames := make([]string, 0, len(stacks.Items))
	var stackErrors []string
	var pendingStacks []string
	for i := range stacks.Items {
		stack := &stacks.Items[i]
		if !stack.GetDeletionTimestamp().IsZero() {
			continue
		}
		targetedStacks[stack.Name] = true
		stackNames = append(stackNames, stack.Name)
		if err := reconcileStackCollector(ctx, stack); err != nil {
			if IsApplicationError(err) {
				pendingStacks = append(pendingStacks, stack.Name)
			} else {
				logger.Error(err, "skipping stack due to reconciliation error", "stack", stack.Name)
				stackErrors = append(stackErrors, fmt.Sprintf("%s: %s", stack.Name, err.Error()))
			}
			continue
		}

		if reason := checkCollectorReady(ctx, stack.Name); reason != "" {
			pendingStacks = append(pendingStacks, fmt.Sprintf("%s (%s)", stack.Name, reason))
		}
	}

	for _, prev := range endpoint.Status.Stacks {
		if !targetedStacks[prev] {
			stack := &v1beta1.Stack{}
			if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: prev}, stack); err != nil {
				if client.IgnoreNotFound(err) == nil {
					if err := cleanupStackCollector(ctx, prev); err != nil {
						logger.Error(err, "failed to clean up collector for deleted stack", "stack", prev)
						stackNames = append(stackNames, prev)
						stackErrors = append(stackErrors, fmt.Sprintf("%s cleanup: %s", prev, err.Error()))
					}
					continue
				}
				logger.Error(err, "failed to get previously matched stack", "stack", prev)
				stackNames = append(stackNames, prev)
				stackErrors = append(stackErrors, fmt.Sprintf("%s: %s", prev, err.Error()))
				continue
			}
			if err := reconcileStackCollector(ctx, stack); err != nil {
				logger.Error(err, "failed to re-reconcile previously matched stack", "stack", prev)
				stackNames = append(stackNames, prev)
				stackErrors = append(stackErrors, fmt.Sprintf("%s: %s", prev, err.Error()))
			}
		}
	}

	slices.Sort(stackNames)
	endpoint.Status.Stacks = slices.Compact(stackNames)

	if len(stackErrors) > 0 {
		endpoint.SetError(fmt.Sprintf("errors in stacks: %s", strings.Join(stackErrors, "; ")))
	} else {
		endpoint.SetError("")
	}

	condition := v1beta1.NewCondition("CollectorsReady", endpoint.GetGeneration())
	if len(pendingStacks) > 0 {
		condition.Fail(fmt.Sprintf("waiting for collectors in: %s", strings.Join(pendingStacks, ", ")))
	}
	endpoint.GetConditions().AppendOrReplace(*condition, func(c v1beta1.Condition) bool {
		return c.Type == "CollectorsReady"
	})

	if len(stackErrors) > 0 {
		return fmt.Errorf("partial failure: %s", strings.Join(stackErrors, "; "))
	}
	return nil
}

func checkCollectorReady(ctx Context, stackName string) string {
	deployment := &appsv1.Deployment{}
	err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stackName,
		Name:      deploymentName,
	}, deployment)
	if err != nil {
		log.FromContext(ctx).V(1).Info("collector deployment not found", "stack", stackName, "error", err)
		return "deployment not found"
	}
	if deployment.Status.ObservedGeneration != deployment.Generation {
		return "waiting for rollout"
	}
	if deployment.Spec.Replicas != nil && deployment.Status.UpdatedReplicas < *deployment.Spec.Replicas {
		return "waiting for updated replicas"
	}
	if deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas {
		for _, cond := range deployment.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable && cond.Status != corev1.ConditionTrue {
				return fmt.Sprintf("not available: %s", cond.Message)
			}
			if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
				return fmt.Sprintf("progress stalled: %s", cond.Message)
			}
		}
		if reason := podWaitingReason(ctx, stackName); reason != "" {
			return reason
		}
		return "waiting for availability"
	}
	return ""
}

func podWaitingReason(ctx Context, stackName string) string {
	podList := &corev1.PodList{}
	if err := ctx.GetClient().List(ctx, podList,
		client.InNamespace(stackName),
		client.MatchingLabels{"app.kubernetes.io/name": deploymentName},
	); err != nil {
		return ""
	}
	for i := range podList.Items {
		for _, cs := range podList.Items[i].Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				if cs.State.Waiting.Message != "" {
					return fmt.Sprintf("%s: %s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
				}
				return cs.State.Waiting.Reason
			}
		}
	}
	return ""
}

func Cleanup(ctx Context, endpoint *v1beta1.OtelExporterEndpoint) error {
	selector, err := selectorFromSpec(endpoint.Spec.StackSelector)
	if err != nil {
		return err
	}

	var stacks v1beta1.StackList
	if err := ctx.GetClient().List(ctx, &stacks, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return err
	}

	var errs []error
	reconciledStacks := map[string]bool{}
	for i := range stacks.Items {
		if !stacks.Items[i].GetDeletionTimestamp().IsZero() {
			continue
		}
		reconciledStacks[stacks.Items[i].Name] = true
		if err := reconcileStackCollector(ctx, &stacks.Items[i]); err != nil {
			errs = append(errs, err)
		}
	}

	for _, stackName := range endpoint.Status.Stacks {
		if reconciledStacks[stackName] {
			continue
		}
		stack := &v1beta1.Stack{}
		if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: stackName}, stack); err != nil {
			if client.IgnoreNotFound(err) == nil {
				if err := cleanupStackCollector(ctx, stackName); err != nil {
					errs = append(errs, err)
				}
				continue
			}
			errs = append(errs, err)
			continue
		}
		if err := reconcileStackCollector(ctx, stack); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func selectorFromSpec(ls *metav1.LabelSelector) (labels.Selector, error) {
	if ls == nil {
		return nil, fmt.Errorf("stackSelector is required")
	}
	return metav1.LabelSelectorAsSelector(ls)
}

func hasTraces(ep *v1beta1.OtelExporterEndpoint) bool {
	return ep.Spec.Traces != nil && ep.Spec.Traces.Endpoint != ""
}

func hasMetrics(ep *v1beta1.OtelExporterEndpoint) bool {
	return ep.Spec.Metrics != nil && ep.Spec.Metrics.Endpoint != ""
}

func hasRealSignal(ep *v1beta1.OtelExporterEndpoint) bool {
	return hasTraces(ep) || hasMetrics(ep)
}

func reconcileStackCollector(ctx Context, stack *v1beta1.Stack) error {
	endpoints, err := findMatchingEndpoints(ctx, stack)
	if err != nil {
		return err
	}

	activeEndpoints := make([]v1beta1.OtelExporterEndpoint, 0, len(endpoints))
	for i := range endpoints {
		if hasRealSignal(&endpoints[i]) {
			activeEndpoints = append(activeEndpoints, endpoints[i])
		}
	}

	otelSettings, err := readOtelSettings(ctx, stack.Name)
	if err != nil {
		return err
	}

	hasSettingsSignal := otelSettings != nil && (otelSettings.TracesEndpoint != "" || otelSettings.MetricsEndpoint != "")
	if len(activeEndpoints) == 0 && !hasSettingsSignal {
		return cleanupStackCollector(ctx, stack.Name)
	}

	if err := ensureNoConflict(ctx, stack.Name); err != nil {
		return err
	}

	if err := ensureAuthSecretReferences(ctx, stack, activeEndpoints); err != nil {
		return err
	}

	inputs, envVars := buildCollectorInputs(activeEndpoints)

	hasTraces, hasMetrics := computeActiveSignals(activeEndpoints, otelSettings)

	collectorConfigYAML, err := generateMergedCollectorConfig(inputs, otelSettings)
	if err != nil {
		return fmt.Errorf("generating collector config: %w", err)
	}

	configMap, _, err := CreateOrUpdate(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      "otel-collector-config",
	},
		func(cm *corev1.ConfigMap) error {
			cm.Data = map[string]string{
				"otel-collector-config.yaml": collectorConfigYAML,
			}
			return nil
		},
		WithOwner[*corev1.ConfigMap](ctx.GetScheme(), stack),
		WithLabels[*corev1.ConfigMap](collectorLabels()),
	)
	if err != nil {
		return fmt.Errorf("creating collector configmap: %w", err)
	}

	secretHashes, err := hashAuthSecrets(ctx, stack.Name, activeEndpoints)
	if err != nil {
		return err
	}

	podAnnotations := map[string]string{
		"config-hash": HashFromConfigMaps(configMap),
	}
	if secretHashes != "" {
		podAnnotations["secret-hash"] = secretHashes
	}

	// Phase 1 (RFC-0008): replicas, resource limits, probes, security context, and scheduling
	// are hardcoded. They will become configurable via Settings keys in a future phase.
	replicas := int32(1)
	_, _, err = CreateOrUpdate(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      deploymentName,
	},
		func(deployment *appsv1.Deployment) error {
			deployment.Spec = appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/name": deploymentName,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      collectorLabels(),
						Annotations: podAnnotations,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "otel-collector",
							Image: collectorImageForPlatform(ctx),
							Args:  []string{"--config=/etc/otel/otel-collector-config.yaml"},
							Env:   envVars,
							Ports: []corev1.ContainerPort{
								{
									Name:          "otlp-http",
									ContainerPort: collectorPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "health",
									ContainerPort: healthCheckPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt32(healthCheckPort),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt32(healthCheckPort),
									},
								},
								InitialDelaySeconds: 3,
								PeriodSeconds:       5,
							},
							SecurityContext: &corev1.SecurityContext{
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								Privileged:               pointer.For(false),
								ReadOnlyRootFilesystem:   pointer.For(true),
								AllowPrivilegeEscalation: pointer.For(false),
								RunAsNonRoot:             pointer.For(true),
								RunAsUser:                pointer.For(int64(65534)),
							},
							VolumeMounts: []corev1.VolumeMount{
								NewVolumeMount("config", "/etc/otel", true),
								NewVolumeMount("tmp", "/tmp", false),
							},
						}},
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: configMap.Name,
										},
									},
								},
							},
							{
								Name: "tmp",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
						},
					},
				},
			}
			return nil
		},
		WithOwner[*appsv1.Deployment](ctx.GetScheme(), stack),
		WithLabels[*appsv1.Deployment](collectorLabels()),
	)
	if err != nil {
		return fmt.Errorf("creating collector deployment: %w", err)
	}

	signalAnnotations := map[string]string{
		SignalTracesAnnotation:  fmt.Sprintf("%t", hasTraces),
		SignalMetricsAnnotation: fmt.Sprintf("%t", hasMetrics),
	}
	_, _, err = CreateOrUpdate(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      serviceName,
	},
		func(svc *corev1.Service) error {
			svc.Spec = corev1.ServiceSpec{
				Selector: map[string]string{
					"app.kubernetes.io/name": deploymentName,
				},
				Ports: []corev1.ServicePort{{
					Name:     "otlp-http",
					Port:     collectorPort,
					Protocol: corev1.ProtocolTCP,
				}},
			}
			if svc.Annotations == nil {
				svc.Annotations = map[string]string{}
			}
			for k, v := range signalAnnotations {
				svc.Annotations[k] = v
			}
			return nil
		},
		WithOwner[*corev1.Service](ctx.GetScheme(), stack),
		WithLabels[*corev1.Service](collectorLabels()),
	)
	if err != nil {
		return fmt.Errorf("creating collector service: %w", err)
	}

	return annotateStackCollectorSignals(ctx, stack, hasTraces, hasMetrics)
}

func annotateStackCollectorSignals(ctx Context, stack *v1beta1.Stack, hasTraces, hasMetrics bool) error {
	value := fmt.Sprintf("traces=%t,metrics=%t", hasTraces, hasMetrics)
	annotations := stack.GetAnnotations()
	if annotations != nil && annotations[collectorSignalAnnotation] == value {
		return nil
	}
	patch := client.MergeFrom(stack.DeepCopy())
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[collectorSignalAnnotation] = value
	stack.SetAnnotations(annotations)
	return ctx.GetClient().Patch(ctx, stack, patch)
}

func removeStackCollectorAnnotation(ctx Context, stackName string) error {
	stack := &v1beta1.Stack{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: stackName}, stack); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		return err
	}
	annotations := stack.GetAnnotations()
	if annotations == nil {
		return nil
	}
	if _, ok := annotations[collectorSignalAnnotation]; !ok {
		return nil
	}
	patch := client.MergeFrom(stack.DeepCopy())
	delete(annotations, collectorSignalAnnotation)
	stack.SetAnnotations(annotations)
	return ctx.GetClient().Patch(ctx, stack, patch)
}

func computeActiveSignals(endpoints []v1beta1.OtelExporterEndpoint, otelSettings *otelSettingsInput) (activeTraces, activeMetrics bool) {
	for i := range endpoints {
		if hasTraces(&endpoints[i]) {
			activeTraces = true
		}
		if hasMetrics(&endpoints[i]) {
			activeMetrics = true
		}
	}
	if otelSettings != nil {
		if otelSettings.TracesEndpoint != "" {
			activeTraces = true
		}
		if otelSettings.MetricsEndpoint != "" {
			activeMetrics = true
		}
	}
	return
}

func findMatchingEndpoints(ctx Context, stack *v1beta1.Stack) ([]v1beta1.OtelExporterEndpoint, error) {
	var allEndpoints v1beta1.OtelExporterEndpointList
	if err := ctx.GetClient().List(ctx, &allEndpoints); err != nil {
		return nil, err
	}

	stackLabels := labels.Set(stack.GetLabels())
	var matching []v1beta1.OtelExporterEndpoint

	for _, ep := range allEndpoints.Items {
		if !ep.GetDeletionTimestamp().IsZero() {
			continue
		}
		selector, err := selectorFromSpec(ep.Spec.StackSelector)
		if err != nil {
			log.FromContext(ctx).Error(err, "invalid stackSelector on OtelExporterEndpoint, skipping", "endpoint", ep.Name)
			continue
		}
		if selector.Matches(stackLabels) {
			matching = append(matching, ep)
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].Name < matching[j].Name
	})
	return matching, nil
}

type authSecretRef struct {
	SecretName string
	SecretKey  string
	Signal     string
	CRDName    string
}

func referencedAuthSecrets(endpoints []v1beta1.OtelExporterEndpoint) []authSecretRef {
	var refs []authSecretRef
	for _, ep := range endpoints {
		crdName := sanitizeName(ep.Name)
		for _, entry := range []struct {
			signal string
			config *v1beta1.OtelSignalConfig
		}{
			{"TRACES", ep.Spec.Traces},
			{"METRICS", ep.Spec.Metrics},
		} {
			if entry.config == nil || entry.config.Auth == nil || entry.config.Auth.Type != "bearer" {
				continue
			}
			secretKey := entry.config.Auth.FromSecretKey
			if secretKey == "" {
				secretKey = "token"
			}
			refs = append(refs, authSecretRef{
				SecretName: entry.config.Auth.FromSecret,
				SecretKey:  secretKey,
				Signal:     entry.signal,
				CRDName:    crdName,
			})
		}
	}
	return refs
}

func buildCollectorInputs(endpoints []v1beta1.OtelExporterEndpoint) ([]collectorInput, []corev1.EnvVar) {
	var inputs []collectorInput
	var envVars []corev1.EnvVar

	refs := referencedAuthSecrets(endpoints)
	refsByKey := map[string]string{}
	for _, ref := range refs {
		envName := fmt.Sprintf("AUTH_%s_%s", envSafe(ref.CRDName), ref.Signal)
		refsByKey[ref.CRDName+"/"+ref.Signal] = envName
		envVars = append(envVars, EnvFromSecret(envName, ref.SecretName, ref.SecretKey))
	}

	for _, ep := range endpoints {
		crdName := sanitizeName(ep.Name)
		ci := collectorInput{Endpoint: &ep}
		ci.TracesEnvAlias = refsByKey[crdName+"/TRACES"]
		ci.MetricsEnvAlias = refsByKey[crdName+"/METRICS"]
		inputs = append(inputs, ci)
	}

	return inputs, envVars
}

func hashAuthSecrets(ctx Context, stackNamespace string, endpoints []v1beta1.OtelExporterEndpoint) (string, error) {
	refs := referencedAuthSecrets(endpoints)
	if len(refs) == 0 {
		return "", nil
	}

	seen := map[string]bool{}
	digest := sha256.New()

	for _, ref := range refs {
		dedupKey := ref.SecretName + "/" + ref.SecretKey
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		secret := &corev1.Secret{}
		err := ctx.GetClient().Get(ctx, types.NamespacedName{
			Name:      ref.SecretName,
			Namespace: stackNamespace,
		}, secret)
		if err != nil {
			return "", fmt.Errorf("auth secret %q in namespace %q: %w", ref.SecretName, stackNamespace, err)
		}
		tokenData, ok := secret.Data[ref.SecretKey]
		if !ok {
			return "", fmt.Errorf("auth secret %q in namespace %q is missing required key %q", ref.SecretName, stackNamespace, ref.SecretKey)
		}
		if _, err := digest.Write(tokenData); err != nil {
			return "", err
		}
	}

	return base64.StdEncoding.EncodeToString(digest.Sum(nil)), nil
}

func readOtelSettings(ctx Context, stackName string) (*otelSettingsInput, error) {
	tracesURL, err := settings.GetURL(ctx, stackName, "opentelemetry", "traces", "dsn")
	if err != nil {
		return nil, err
	}
	metricsURL, err := settings.GetURL(ctx, stackName, "opentelemetry", "metrics", "dsn")
	if err != nil {
		return nil, err
	}

	if tracesURL == nil && metricsURL == nil {
		return nil, nil
	}

	input := &otelSettingsInput{}
	if tracesURL != nil {
		input.TracesEndpoint = tracesURL.String()
	}
	if metricsURL != nil {
		input.MetricsEndpoint = metricsURL.String()
	}
	return input, nil
}

func deleteIfManaged[T client.Object](ctx Context, name types.NamespacedName) error {
	var t T
	t = reflect.New(reflect.TypeOf(t).Elem()).Interface().(T)
	if err := ctx.GetClient().Get(ctx, name, t); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		return err
	}
	if t.GetLabels()[managedByLabel] != managedByValue {
		return nil
	}
	LogDeletion(ctx, t, "deleteIfManaged")
	return ctx.GetClient().Delete(ctx, t)
}

func checkNotUnmanaged[T client.Object](ctx Context, name types.NamespacedName) error {
	var t T
	t = reflect.New(reflect.TypeOf(t).Elem()).Interface().(T)
	if err := ctx.GetClient().Get(ctx, name, t); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		return err
	}
	if t.GetLabels()[managedByLabel] != managedByValue {
		return fmt.Errorf("resource %s/%s already exists and is not managed by otelexporterendpoint", name.Namespace, name.Name)
	}
	return nil
}

func otelRefName(stackName, secretName string) string {
	name := fmt.Sprintf("%s--otel--%s", stackName, secretName)
	if len(name) > 253 {
		h := sha256.Sum256([]byte(name))
		name = name[:240] + "-" + hex.EncodeToString(h[:6])
	}
	return name
}

func ensureAuthSecretReferences(ctx Context, stack *v1beta1.Stack, endpoints []v1beta1.OtelExporterEndpoint) error {
	wanted := map[string]bool{}
	for _, ref := range referencedAuthSecrets(endpoints) {
		if wanted[ref.SecretName] {
			continue
		}
		wanted[ref.SecretName] = true

		existing := &corev1.Secret{}
		if err := ctx.GetClient().Get(ctx, types.NamespacedName{
			Name:      ref.SecretName,
			Namespace: stack.Name,
		}, existing); err == nil {
			continue
		} else if client.IgnoreNotFound(err) != nil {
			return err
		}

		refName := otelRefName(stack.Name, ref.SecretName)
		rr, _, err := CreateOrUpdate(ctx, types.NamespacedName{
			Name: refName,
		}, func(rr *v1beta1.ResourceReference) error {
			rr.Spec.Stack = stack.Name
			rr.Spec.Name = ref.SecretName
			rr.Spec.GroupVersionKind = &metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Secret",
			}
			return nil
		},
			WithOwner[*v1beta1.ResourceReference](ctx.GetScheme(), stack),
		)
		if err != nil {
			return fmt.Errorf("creating ResourceReference for secret %q in stack %q: %w", ref.SecretName, stack.Name, err)
		}
		if !rr.Status.Ready {
			return NewPendingError()
		}
	}

	wantedRefNames := map[string]bool{}
	for secretName := range wanted {
		wantedRefNames[otelRefName(stack.Name, secretName)] = true
	}

	return reconcileOtelResourceReferences(ctx, stack.Name, wantedRefNames)
}

func ensureNoConflict(ctx Context, namespace string) error {
	if err := checkNotUnmanaged[*corev1.ConfigMap](ctx, types.NamespacedName{
		Name: "otel-collector-config", Namespace: namespace,
	}); err != nil {
		return err
	}
	if err := checkNotUnmanaged[*appsv1.Deployment](ctx, types.NamespacedName{
		Name: deploymentName, Namespace: namespace,
	}); err != nil {
		return err
	}
	return checkNotUnmanaged[*corev1.Service](ctx, types.NamespacedName{
		Name: serviceName, Namespace: namespace,
	})
}

func cleanupStackCollector(ctx Context, namespace string) error {
	if err := deleteIfManaged[*corev1.Service](ctx, types.NamespacedName{
		Name: serviceName, Namespace: namespace,
	}); err != nil {
		return err
	}
	if err := deleteIfManaged[*appsv1.Deployment](ctx, types.NamespacedName{
		Name: deploymentName, Namespace: namespace,
	}); err != nil {
		return err
	}
	if err := deleteIfManaged[*corev1.ConfigMap](ctx, types.NamespacedName{
		Name: "otel-collector-config", Namespace: namespace,
	}); err != nil {
		return err
	}
	if err := reconcileOtelResourceReferences(ctx, namespace, nil); err != nil {
		return err
	}
	return removeStackCollectorAnnotation(ctx, namespace)
}

func reconcileOtelResourceReferences(ctx Context, stackName string, wanted map[string]bool) error {
	var allRefs v1beta1.ResourceReferenceList
	if err := ctx.GetClient().List(ctx, &allRefs, client.MatchingFields{
		"stack": stackName,
	}); err != nil {
		return err
	}
	prefix := stackName + "--otel--"
	for i := range allRefs.Items {
		rr := &allRefs.Items[i]
		if !strings.HasPrefix(rr.Name, prefix) {
			continue
		}
		if wanted[rr.Name] {
			continue
		}
		if err := ctx.GetClient().Delete(ctx, rr); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

func collectorImageForPlatform(ctx Context) string {
	if img := ctx.GetPlatform().CollectorImage; img != "" {
		return img
	}
	return DefaultCollectorImage
}

func envSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		if r >= 'a' && r <= 'z' {
			return r - 32
		}
		return '_'
	}, s)
}

func isOtelSettingsKey(key string) bool {
	parts := strings.Split(key, ".")
	if len(parts) < 3 {
		return false
	}
	if parts[0] != "opentelemetry" && parts[0] != "*" {
		return false
	}
	signal := parts[1]
	if signal != "traces" && signal != "metrics" && signal != "*" {
		return false
	}
	return true
}

func isCollectorResource(obj client.Object) bool {
	return obj.GetLabels()[managedByLabel] == managedByValue
}

func enqueueAllEndpoints(ctx Context) []reconcile.Request {
	var endpoints v1beta1.OtelExporterEndpointList
	if err := ctx.GetClient().List(ctx, &endpoints); err != nil {
		log.FromContext(ctx).Error(err, "failed to list OtelExporterEndpoints for requeue")
		return nil
	}
	return MapObjectToReconcileRequests(
		collectionutils.Map(endpoints.Items, func(e v1beta1.OtelExporterEndpoint) *v1beta1.OtelExporterEndpoint { return &e })...,
	)
}

func init() {
	Init(
		WithStdReconciler(Reconcile,
			WithFinalizer[*v1beta1.OtelExporterEndpoint](collectorFinalizer, Cleanup),
			WithWatch[*v1beta1.OtelExporterEndpoint, *v1beta1.Stack](func(ctx Context, _ *v1beta1.Stack) []reconcile.Request {
				return enqueueAllEndpoints(ctx)
			}),
			WithWatch[*v1beta1.OtelExporterEndpoint, *v1beta1.ResourceReference](func(ctx Context, rr *v1beta1.ResourceReference) []reconcile.Request {
				if !strings.HasPrefix(rr.Name, rr.Spec.Stack+"--otel--") {
					return nil
				}
				return enqueueAllEndpoints(ctx)
			}),
			WithRaw[*v1beta1.OtelExporterEndpoint](func(ctx Context, b *builder.Builder) error {
				b.Watches(&v1beta1.Settings{}, handler.EnqueueRequestsFromMapFunc(
					func(_ context.Context, obj client.Object) []reconcile.Request {
						s := obj.(*v1beta1.Settings)
						if !isOtelSettingsKey(s.Spec.Key) {
							return nil
						}
						return enqueueAllEndpoints(ctx)
					},
				))
				return nil
			}),
			WithRaw[*v1beta1.OtelExporterEndpoint](func(ctx Context, b *builder.Builder) error {
				collectorPredicate := predicate.NewPredicateFuncs(isCollectorResource)
				enqueueHandler := handler.EnqueueRequestsFromMapFunc(
					func(_ context.Context, _ client.Object) []reconcile.Request {
						return enqueueAllEndpoints(ctx)
					},
				)
				b.Watches(&corev1.ConfigMap{}, enqueueHandler, builder.WithPredicates(collectorPredicate))
				b.Watches(&appsv1.Deployment{}, enqueueHandler, builder.WithPredicates(collectorPredicate))
				b.Watches(&corev1.Service{}, enqueueHandler, builder.WithPredicates(collectorPredicate))
				return nil
			}),
		),
	)
}
