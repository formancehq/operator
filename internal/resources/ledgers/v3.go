package ledgers

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/auths"
	"github.com/formancehq/operator/v3/internal/resources/gatewaygrpcapis"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

const (
	ledgerV3Threshold             = "v3.0.0-alpha"
	ledgerV3ClusterReadyCondition = "LedgerV3ClusterReady"
	ledgerV3PreviewReadyCondition = "LedgerV3PreviewReady"
	ledgerV3PreviewLabel          = "formance.com/ledger-v3-preview"
	ledgerV3GRPCPort              = int32(8888)
	ledgerV3HTTPPort              = int32(9000)
	ledgerV3PublicGRPCService     = "ledger.BucketService"
)

var (
	ledgerV3ClusterGVK = schema.GroupVersionKind{
		Group:   "ledger.formance.com",
		Version: "v1alpha1",
		Kind:    "Cluster",
	}
	ledgerV3ClusterAvailable     bool
	ledgerV3CertManagerAvailable bool
)

//+kubebuilder:rbac:groups=ledger.formance.com,resources=clusters,verbs=get;list;watch;create;update;patch;delete

func isLedgerV3(version string) bool {
	normalizedVersion := version
	if !strings.HasPrefix(normalizedVersion, "v") {
		normalizedVersion = "v" + normalizedVersion
	}
	return semver.IsValid(normalizedVersion) && semver.Compare(normalizedVersion, ledgerV3Threshold) > 0
}

func newV3Cluster() *unstructured.Unstructured {
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(ledgerV3ClusterGVK)
	return cluster
}

func withLedgerV3ClusterWatch() core.ReconcilerOption[*v1beta1.Ledger] {
	return func(options *core.ReconcilerOptions[*v1beta1.Ledger]) {
		options.Raws = append(options.Raws, func(ctx core.Context, b *builder.Builder) error {
			crds := &apiextensionsv1.CustomResourceDefinitionList{}
			if err := ctx.GetAPIReader().List(ctx, crds); err != nil {
				return err
			}

			ledgerV3ClusterAvailable = watchLedgerV3Resource(ctx, b, options, crds, ledgerV3ClusterGVK)
			issuerAvailable := watchLedgerV3Resource(ctx, b, options, crds, ledgerV3IssuerGVK)
			certificateAvailable := watchLedgerV3Resource(ctx, b, options, crds, ledgerV3CertificateGVK)
			ledgerV3CertManagerAvailable = issuerAvailable && certificateAvailable
			return nil
		})
	}
}

func watchLedgerV3Resource(
	ctx core.Context,
	b *builder.Builder,
	options *core.ReconcilerOptions[*v1beta1.Ledger],
	crds *apiextensionsv1.CustomResourceDefinitionList,
	gvk schema.GroupVersionKind,
) bool {
	for _, crd := range crds.Items {
		if crd.Spec.Group != gvk.Group || crd.Spec.Names.Kind != gvk.Kind {
			continue
		}
		for _, version := range crd.Spec.Versions {
			if version.Name == gvk.Version && version.Served {
				resource := newLedgerV3Resource(gvk)
				options.Owns[resource] = nil
				b.Owns(resource)
				log.FromContext(ctx).Info("Ledger v3 dependency CRD is available", "gvk", gvk)
				return true
			}
		}
	}

	log.FromContext(ctx).Info("Ledger v3 dependency CRD is not available", "gvk", gvk)
	return false
}

func reconcileV3(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string) error {
	if !ledgerV3ClusterAvailable {
		setLedgerV3Condition(ledger, metav1.ConditionFalse, "OperatorUnavailable", "Ledger v3 Cluster CRD is not installed")
		return core.NewPendingError().WithMessage("Ledger v3 operator unavailable: Cluster CRD is not installed")
	}

	clearLegacyLedgerConditions(ledger)

	hasLegacyResources, err := legacyLedgerResourcesExist(ctx, stack)
	if err != nil {
		return err
	}
	if hasLegacyResources {
		setLedgerV3Condition(ledger, metav1.ConditionFalse, "MigrationRequired", "Legacy Ledger resources exist; an explicit v2 to v3 migration is required")
		return core.NewPendingError().WithMessage("migration required before switching Ledger from v2 to v3")
	}

	tlsReady, tlsMessage, tlsCAHash, err := createOrUpdateV3TLSResources(ctx, stack, ledger, false)
	if err != nil {
		setLedgerV3Condition(ledger, metav1.ConditionFalse, "TLSReconcileFailed", err.Error())
		return err
	}

	cluster, err := createOrUpdateV3Cluster(ctx, stack, ledger, version, false, tlsCAHash)
	if err != nil {
		setLedgerV3Condition(ledger, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return err
	}
	if !tlsReady {
		setLedgerV3Condition(ledger, metav1.ConditionFalse, "TLSCertificatePending", tlsMessage)
		return core.NewPendingError().WithMessage("Ledger v3 TLS is not ready: %s", tlsMessage)
	}
	if err := gatewayhttpapis.Create(ctx, ledger, gatewayhttpapis.WithHealthCheckEndpoint("livez")); err != nil {
		return err
	}
	if err := gatewaygrpcapis.Create(ctx, ledger,
		gatewaygrpcapis.WithGRPCServices(ledgerV3PublicGRPCService),
		gatewaygrpcapis.WithPort(ledgerV3GRPCPort),
		gatewaygrpcapis.WithBackendRef(ledgerV3GRPCBackendRef(stack.Name)),
	); err != nil {
		return err
	}

	ready, message, err := isV3ClusterReady(cluster)
	if err != nil {
		return err
	}
	if !ready {
		setLedgerV3Condition(ledger, metav1.ConditionFalse, "Pending", message)
		return core.NewPendingError().WithMessage("Ledger v3 Cluster is not ready: %s", message)
	}

	setLedgerV3Condition(ledger, metav1.ConditionTrue, "Running", message)
	return nil
}

func clearLegacyLedgerConditions(ledger *v1beta1.Ledger) {
	conditions := ledger.GetConditions()
	for _, conditionType := range []string{
		"DatabaseReady",
		"DeploymentReady",
		"PodDisruptionBudget",
		"PodDisruptionBudgetConfigured",
	} {
		for conditions.Get(conditionType) != nil {
			conditions.Delete(v1beta1.ConditionTypeMatch(conditionType))
		}
	}
}

func setLedgerV3Condition(ledger *v1beta1.Ledger, status metav1.ConditionStatus, reason, message string) {
	ledger.GetConditions().AppendOrReplace(v1beta1.Condition{
		Type:               ledgerV3ClusterReadyCondition,
		Status:             status,
		ObservedGeneration: ledger.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}, v1beta1.ConditionTypeMatch(ledgerV3ClusterReadyCondition))
}

func legacyLedgerResourcesExist(ctx core.Context, stack *v1beta1.Stack) (bool, error) {
	for _, name := range []string{"ledger", "ledger-worker", "ledger-write", "ledger-read", "ledger-gateway"} {
		deployment := &appsv1.Deployment{}
		err := ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: name}, deployment)
		if err == nil {
			return true, nil
		}
		if !apierrors.IsNotFound(err) {
			return false, err
		}
	}

	database := &v1beta1.Database{}
	err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: core.GetObjectName(stack.Name, "ledger")}, database)
	if err == nil {
		return true, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	return false, nil
}

func getV3Cluster(ctx core.Context, stack *v1beta1.Stack) (*unstructured.Unstructured, bool, error) {
	cluster := newV3Cluster()
	err := ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
	if apierrors.IsNotFound(err) {
		return cluster, false, nil
	}
	return cluster, err == nil, err
}

func normalizeLedgerV3Replicas(configured int32) (int32, bool, error) {
	if configured < 1 {
		return 0, false, fmt.Errorf("deployments.ledger.replicas must be positive, got %d", configured)
	}
	if configured%2 == 0 {
		return configured + 1, true, nil
	}
	return configured, false, nil
}

func createOrUpdateV3Cluster(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string, preview bool, tlsCAHash string) (*unstructured.Unstructured, error) {
	image, err := registries.GetFormanceImage(ctx, stack, "ledger", version)
	if err != nil {
		return nil, err
	}

	configuredReplicas, err := settings.GetInt32OrDefault(ctx, stack.Name, 3, "deployments", "ledger", "replicas")
	if err != nil {
		return nil, err
	}
	replicas, normalized, err := normalizeLedgerV3Replicas(configuredReplicas)
	if err != nil {
		return nil, err
	}
	if normalized {
		log.FromContext(ctx).Info("Normalized Ledger v3 replicas to an odd number",
			"setting", "deployments.ledger.replicas",
			"configuredReplicas", configuredReplicas,
			"replicas", replicas)
	}
	resourceRequirements, err := settings.GetResourceRequirements(ctx, stack.Name,
		"deployments", "ledger", "containers", "ledger", "resource-requirements")
	if err != nil {
		return nil, err
	}

	monitoringConfiguration, err := settings.GetOpenTelemetryConfiguration(ctx, stack.Name, "ledger")
	if err != nil {
		return nil, err
	}
	authConfiguration, err := auths.GetProtectedConfiguration(ctx, stack, "ledger", ledger.Spec.Auth)
	if err != nil {
		return nil, err
	}
	extraEnv, err := buildV3ExtraEnv(stack, ledger)
	if err != nil {
		return nil, err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return nil, err
	}

	cluster := newV3Cluster()
	cluster.SetNamespace(stack.Name)
	cluster.SetName(stack.Name)
	_, err = controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), cluster, func() error {
		labels := cluster.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[v1beta1.StackLabel] = stack.Name
		labels[v1beta1.LedgerV3Label] = "true"
		if preview {
			labels[ledgerV3PreviewLabel] = "true"
		} else {
			delete(labels, ledgerV3PreviewLabel)
		}
		cluster.SetLabels(labels)

		if err := controllerutil.SetControllerReference(ledger, cluster, ctx.GetScheme()); err != nil {
			return err
		}

		if err := unstructured.SetNestedField(cluster.Object, imageRepository(image), "spec", "image", "repository"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(cluster.Object, image.Version, "spec", "image", "tag"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(cluster.Object, int64(replicas), "spec", "replicas"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(cluster.Object, stack.Name, "spec", "clusterID"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(cluster.Object, stack.Spec.Debug || ledger.Spec.Debug, "spec", "debug"); err != nil {
			return err
		}
		// Configure TLS from the first Cluster revision. Pods can wait for the
		// cert-manager Secret, but must never bootstrap a plaintext Raft cluster
		// that is switched to TLS by a later StatefulSet revision.
		if err := unstructured.SetNestedMap(cluster.Object, ledgerV3TLSSpec(stack.Name), "spec", "tls"); err != nil {
			return err
		}
		podAnnotations, _, err := unstructured.NestedStringMap(cluster.Object, "spec", "podAnnotations")
		if err != nil {
			return err
		}
		if podAnnotations == nil {
			podAnnotations = map[string]string{}
		}
		if tlsCAHash != "" {
			podAnnotations[ledgerV3TLSCAHashAnnotation] = tlsCAHash
		} else {
			delete(podAnnotations, ledgerV3TLSCAHashAnnotation)
		}
		if len(podAnnotations) > 0 {
			if err := unstructured.SetNestedStringMap(cluster.Object, podAnnotations, "spec", "podAnnotations"); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(cluster.Object, "spec", "podAnnotations")
		}
		additionalLabels := map[string]string{}
		if preview {
			additionalLabels["app.kubernetes.io/name"] = "ledger-v3-preview"
			additionalLabels[ledgerV3PreviewLabel] = "true"
		}
		if len(additionalLabels) > 0 {
			if err := unstructured.SetNestedStringMap(cluster.Object, additionalLabels, "spec", "additionalLabels"); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(cluster.Object, "spec", "additionalLabels")
		}

		if hasResourceRequirements(resourceRequirements) {
			resources, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resourceRequirements)
			if err != nil {
				return err
			}
			if err := unstructured.SetNestedMap(cluster.Object, resources, "spec", "resources"); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(cluster.Object, "spec", "resources")
		}

		if len(image.PullSecrets) > 0 {
			pullSecrets := make([]any, 0, len(image.PullSecrets))
			for _, pullSecret := range image.PullSecrets {
				pullSecrets = append(pullSecrets, map[string]any{"name": pullSecret.Name})
			}
			if err := unstructured.SetNestedSlice(cluster.Object, pullSecrets, "spec", "imagePullSecrets"); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(cluster.Object, "spec", "imagePullSecrets")
		}

		if len(extraEnv) > 0 {
			if err := unstructured.SetNestedSlice(cluster.Object, extraEnv, "spec", "extraEnv"); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(cluster.Object, "spec", "extraEnv")
		}

		if monitoringConfiguration != nil {
			if err := unstructured.SetNestedMap(cluster.Object, ledgerV3MonitoringSpec(monitoringConfiguration), "spec", "monitoring"); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(cluster.Object, "spec", "monitoring")
		}

		if authConfiguration != nil {
			if err := unstructured.SetNestedMap(cluster.Object, ledgerV3AuthSpec(authConfiguration), "spec", "auth"); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(cluster.Object, "spec", "auth")
		}

		if serviceAccountName != "" {
			if err := unstructured.SetNestedField(cluster.Object, false, "spec", "serviceAccount", "create"); err != nil {
				return err
			}
			if err := unstructured.SetNestedField(cluster.Object, serviceAccountName, "spec", "serviceAccount", "name"); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(cluster.Object, "spec", "serviceAccount")
		}

		return nil
	})
	return cluster, err
}

func hasResourceRequirements(resources *corev1.ResourceRequirements) bool {
	return resources != nil && (len(resources.Limits) > 0 || len(resources.Requests) > 0 || len(resources.Claims) > 0)
}

func imageRepository(image *registries.ImageConfiguration) string {
	if image.Registry == "" {
		return image.Image
	}
	return strings.TrimSuffix(image.Registry, "/") + "/" + image.Image
}

func buildV3ExtraEnv(stack *v1beta1.Stack, ledger *v1beta1.Ledger) ([]any, error) {
	env := core.GetDevEnvVars(stack, ledger)

	ret := make([]any, 0, len(env))
	for i := range env {
		value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&env[i])
		if err != nil {
			return nil, err
		}
		ret = append(ret, value)
	}
	return ret, nil
}

func ledgerV3AuthSpec(configuration *auths.ProtectedAuthConfiguration) map[string]any {
	spec := map[string]any{
		"enabled": true,
		"issuer":  configuration.Issuer,
	}
	if len(configuration.Issuers) > 0 {
		issuers := make([]any, 0, len(configuration.Issuers))
		for _, issuer := range configuration.Issuers {
			issuers = append(issuers, issuer)
		}
		spec["issuers"] = issuers
	}
	if configuration.ReadKeySetMaxRetries != 0 {
		spec["readKeySetMaxRetries"] = int64(configuration.ReadKeySetMaxRetries)
	}
	if configuration.CheckScopes {
		spec["checkScopes"] = true
		spec["service"] = configuration.Service
	}
	return spec
}

func ledgerV3MonitoringSpec(configuration *settings.OpenTelemetryConfiguration) map[string]any {
	spec := map[string]any{
		"serviceName": configuration.ServiceName,
	}
	if len(configuration.Attributes) > 0 {
		attributes := make([]string, 0, len(configuration.Attributes))
		for key, value := range configuration.Attributes {
			attributes = append(attributes, fmt.Sprintf("%s=%s", key, value))
		}
		slices.Sort(attributes)
		spec["attributes"] = strings.Join(attributes, ",")
	}
	if configuration.Traces != nil {
		traces := ledgerV3MonitoringSignalSpec(configuration.Traces)
		traces["batch"] = "true"
		spec["traces"] = traces
	}
	if configuration.Metrics != nil {
		metrics := ledgerV3MonitoringSignalSpec(configuration.Metrics)
		metrics["runtime"] = true
		spec["metrics"] = metrics
	}
	if configuration.Logs != nil {
		spec["logs"] = ledgerV3MonitoringSignalSpec(configuration.Logs)
	}
	return spec
}

func ledgerV3MonitoringSignalSpec(configuration *settings.OpenTelemetrySignalConfiguration) map[string]any {
	return map[string]any{
		"enabled":  true,
		"exporter": "otlp",
		"endpoint": configuration.Endpoint,
		"port":     configuration.Port,
		"insecure": strconv.FormatBool(configuration.Insecure),
		"mode":     configuration.Mode,
	}
}

func isV3ClusterReady(cluster *unstructured.Unstructured) (bool, string, error) {
	phase, _, err := unstructured.NestedString(cluster.Object, "status", "phase")
	if err != nil {
		return false, "", err
	}
	readyReplicas, _, err := unstructured.NestedInt64(cluster.Object, "status", "readyReplicas")
	if err != nil {
		return false, "", err
	}
	observedGeneration, _, err := unstructured.NestedInt64(cluster.Object, "status", "observedGeneration")
	if err != nil {
		return false, "", err
	}
	replicas, found, err := unstructured.NestedInt64(cluster.Object, "spec", "replicas")
	if err != nil {
		return false, "", err
	}
	if !found {
		replicas = 3
	}

	message := fmt.Sprintf("phase=%s readyReplicas=%d/%d observedGeneration=%d/%d", phase, readyReplicas, replicas, observedGeneration, cluster.GetGeneration())
	return phase == "Running" && readyReplicas == replicas && observedGeneration == cluster.GetGeneration(), message, nil
}
