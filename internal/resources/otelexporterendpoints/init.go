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
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/formancehq/go-libs/v2/collectionutils"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

//+kubebuilder:rbac:groups=formance.com,resources=otelexporterendpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=otelexporterendpoints/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=otelexporterendpoints/finalizers,verbs=update

const (
	defaultCollectorImage = "otel/opentelemetry-collector-contrib:0.151.0"
	deploymentName        = "otel-collector"
	serviceName           = "otel-collector"
	collectorPort         = 4318

	managedByLabel     = "formance.com/managed-by"
	managedByValue     = "otel-exporter-endpoint"
	collectorFinalizer = "otelexporterendpoint.formance.com/finalizer"
)

var managedLabels = map[string]string{
	managedByLabel: managedByValue,
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

	stackNames := make([]string, 0, len(stacks.Items))
	for i := range stacks.Items {
		stack := &stacks.Items[i]
		if !stack.GetDeletionTimestamp().IsZero() {
			continue
		}
		if err := reconcileStackCollector(ctx, stack); err != nil {
			return fmt.Errorf("reconciling collector for stack %s: %w", stack.Name, err)
		}
		stackNames = append(stackNames, stack.Name)
	}

	slices.Sort(stackNames)
	endpoint.Status.Stacks = stackNames
	return nil
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

	for i := range stacks.Items {
		if err := reconcileStackCollector(ctx, &stacks.Items[i]); err != nil {
			return err
		}
	}
	return nil
}

func selectorFromSpec(ls *metav1.LabelSelector) (labels.Selector, error) {
	if ls == nil {
		return labels.Everything(), nil
	}
	return metav1.LabelSelectorAsSelector(ls)
}

func reconcileStackCollector(ctx Context, stack *v1beta1.Stack) error {
	endpoints, err := findMatchingEndpoints(ctx, stack)
	if err != nil {
		return err
	}

	if len(endpoints) == 0 {
		return cleanupStackCollector(ctx, stack.Name)
	}

	inputs, envVars := buildCollectorInputs(endpoints)

	otelSettings, err := readOtelSettings(ctx, stack.Name)
	if err != nil {
		return err
	}

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
		WithLabels[*corev1.ConfigMap](managedLabels),
	)
	if err != nil {
		return fmt.Errorf("creating collector configmap: %w", err)
	}

	secretHashes, err := hashAuthSecrets(ctx, stack.Name, endpoints)
	if err != nil {
		return err
	}

	annotations := map[string]string{
		"config-hash": HashFromConfigMaps(configMap),
	}
	if secretHashes != "" {
		annotations["secret-hash"] = secretHashes
	}

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
						Labels: map[string]string{
							"app.kubernetes.io/name": deploymentName,
						},
						Annotations: annotations,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "otel-collector",
							Image: collectorImageForPlatform(ctx),
							Args:  []string{"--config=/etc/otel/otel-collector-config.yaml"},
							Env:   envVars,
							Ports: []corev1.ContainerPort{{
								Name:          "otlp-http",
								ContainerPort: collectorPort,
								Protocol:      corev1.ProtocolTCP,
							}},
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
		WithLabels[*appsv1.Deployment](managedLabels),
	)
	if err != nil {
		return fmt.Errorf("creating collector deployment: %w", err)
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
			return nil
		},
		WithLabels[*corev1.Service](managedLabels),
	)
	if err != nil {
		return fmt.Errorf("creating collector service: %w", err)
	}

	return nil
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
			refs = append(refs, authSecretRef{
				SecretName: entry.config.Auth.FromSecret,
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
		envVars = append(envVars, EnvFromSecret(envName, ref.SecretName, "token"))
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
		if seen[ref.SecretName] {
			continue
		}
		seen[ref.SecretName] = true

		secret := &corev1.Secret{}
		err := ctx.GetClient().Get(ctx, types.NamespacedName{
			Name:      ref.SecretName,
			Namespace: stackNamespace,
		}, secret)
		if err != nil {
			return "", fmt.Errorf("auth secret %q not found in namespace %q: %w", ref.SecretName, stackNamespace, err)
		}
		if err := json.NewEncoder(digest).Encode(secret.Data); err != nil {
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

func cleanupStackCollector(ctx Context, namespace string) error {
	if err := DeleteIfExists[*corev1.Service](ctx, types.NamespacedName{
		Name: serviceName, Namespace: namespace,
	}); err != nil {
		return err
	}
	if err := DeleteIfExists[*appsv1.Deployment](ctx, types.NamespacedName{
		Name: deploymentName, Namespace: namespace,
	}); err != nil {
		return err
	}
	return DeleteIfExists[*corev1.ConfigMap](ctx, types.NamespacedName{
		Name: "otel-collector-config", Namespace: namespace,
	})
}

func collectorImageForPlatform(ctx Context) string {
	if img := ctx.GetPlatform().CollectorImage; img != "" {
		return img
	}
	return defaultCollectorImage
}

func envSafe(s string) string {
	replacer := func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		if r >= 'a' && r <= 'z' {
			return r - 32
		}
		return '_'
	}
	result := make([]rune, 0, len(s))
	for _, r := range s {
		result = append(result, replacer(r))
	}
	return string(result)
}

func isManagedResource(obj client.Object) bool {
	l := obj.GetLabels()
	return l != nil && l[managedByLabel] == managedByValue
}

func enqueueAllEndpoints(ctx Context) []reconcile.Request {
	var endpoints v1beta1.OtelExporterEndpointList
	if err := ctx.GetClient().List(ctx, &endpoints); err != nil {
		return nil
	}
	return MapObjectToReconcileRequests(
		Map(endpoints.Items, func(e v1beta1.OtelExporterEndpoint) *v1beta1.OtelExporterEndpoint { return &e })...,
	)
}

func init() {
	Init(
		WithStdReconciler(Reconcile,
			WithFinalizer[*v1beta1.OtelExporterEndpoint](collectorFinalizer, Cleanup),
			WithWatch[*v1beta1.OtelExporterEndpoint, *v1beta1.Stack](func(ctx Context, _ *v1beta1.Stack) []reconcile.Request {
				return enqueueAllEndpoints(ctx)
			}),
			WithWatch[*v1beta1.OtelExporterEndpoint, *v1beta1.Settings](func(ctx Context, _ *v1beta1.Settings) []reconcile.Request {
				return enqueueAllEndpoints(ctx)
			}),
			WithRaw[*v1beta1.OtelExporterEndpoint](func(ctx Context, b *builder.Builder) error {
				managedPredicate := predicate.NewPredicateFuncs(isManagedResource)
				enqueueHandler := handler.EnqueueRequestsFromMapFunc(
					func(_ context.Context, _ client.Object) []reconcile.Request {
						return enqueueAllEndpoints(ctx)
					},
				)
				b.Watches(&corev1.ConfigMap{}, enqueueHandler, builder.WithPredicates(managedPredicate))
				b.Watches(&appsv1.Deployment{}, enqueueHandler, builder.WithPredicates(managedPredicate))
				b.Watches(&corev1.Service{}, enqueueHandler, builder.WithPredicates(managedPredicate))
				return nil
			}),
		),
	)
}
