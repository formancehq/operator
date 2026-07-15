package ledgers

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

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

var ledgerV3RequiredVerbs = []string{"get", "list", "watch", "create", "update", "patch", "delete"}

//+kubebuilder:rbac:groups=ledger.formance.com,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=ledgerconfigurations,verbs=get;list;watch
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=selfsubjectaccessreviews,verbs=create

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
				ledgerV3ClusterAvailable = false
				ledgerV3CertManagerAvailable = false
				log.FromContext(ctx).Info("Ledger v3 capability is unavailable; continuing without it",
					"error", err)
				return nil
			}

			ledgerV3ClusterAvailable = watchLedgerV3Resource(ctx, b, options, crds, ledgerV3ClusterGVK)
			issuerAvailable := watchLedgerV3Resource(ctx, b, options, crds, ledgerV3IssuerGVK)
			certificateAvailable := watchLedgerV3Resource(ctx, b, options, crds, ledgerV3CertificateGVK)
			ledgerV3CertManagerAvailable = issuerAvailable && certificateAvailable
			return nil
		})
	}
}

func withLedgerConfigurationWatch() core.ReconcilerOption[*v1beta1.Ledger] {
	return core.WithWatch[*v1beta1.Ledger, *v1beta1.LedgerConfiguration](
		func(ctx core.Context, configuration *v1beta1.LedgerConfiguration) []reconcile.Request {
			if configuration.IsWildcard() {
				return core.BuildReconcileRequests(
					ctx,
					ctx.GetClient(),
					ctx.GetScheme(),
					&v1beta1.Ledger{},
				)
			}

			requests := make([]reconcile.Request, 0)
			for _, stack := range configuration.GetStacks() {
				requests = append(requests, core.BuildReconcileRequests(
					ctx,
					ctx.GetClient(),
					ctx.GetScheme(),
					&v1beta1.Ledger{},
					client.MatchingFields{"stack": stack},
				)...)
			}
			return requests
		},
	)
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
				resourceList := &unstructured.UnstructuredList{}
				resourceList.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
				if err := ctx.GetAPIReader().List(ctx, resourceList, client.Limit(1)); err != nil {
					log.FromContext(ctx).Info("Ledger v3 dependency is inaccessible; continuing without it",
						"gvk", gvk,
						"error", err)
					return false
				}
				if !canAccessLedgerV3Resource(ctx, gvk, crd.Spec.Names.Plural) {
					return false
				}

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

func canAccessLedgerV3Resource(ctx core.Context, gvk schema.GroupVersionKind, resource string) bool {
	for _, verb := range ledgerV3RequiredVerbs {
		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:    gvk.Group,
					Version:  gvk.Version,
					Resource: resource,
					Verb:     verb,
				},
			},
		}
		if err := ctx.GetClient().Create(ctx, review); err != nil {
			log.FromContext(ctx).Info("Ledger v3 dependency access review failed; continuing without it",
				"gvk", gvk,
				"verb", verb,
				"error", err)
			return false
		}
		if !review.Status.Allowed {
			log.FromContext(ctx).Info("Ledger v3 dependency permission is unavailable; continuing without it",
				"gvk", gvk,
				"verb", verb,
				"reason", review.Status.Reason)
			return false
		}
	}
	return true
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
	if err := gatewayhttpapis.Create(ctx, ledger,
		gatewayhttpapis.WithHealthCheckEndpoint("livez"),
		gatewayhttpapis.WithRules(gatewayhttpapis.RuleSecuredWithBackend("", ledgerV3HTTPBackendRef(stack.Name))),
	); err != nil {
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
	baseSpec, err := ledgerV3BaseSpec(ctx, stack.Name)
	if err != nil {
		return nil, err
	}

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
	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return nil, err
	}
	topologySpreadConstraints, err := settings.GetBool(ctx, stack.Name,
		"deployments", "ledger", "topology-spread-constraints")
	if err != nil {
		return nil, err
	}
	desiredSpec := composeLedgerV3ClusterSpec(baseSpec, ledgerV3SpecOverrides{
		ImageRepository:           imageRepository(image),
		ImageTag:                  image.Version,
		ImagePullSecrets:          image.PullSecrets,
		Replicas:                  replicas,
		ClusterID:                 stack.Name,
		Debug:                     stack.Spec.Debug || ledger.Spec.Debug,
		TLSSecretName:             ledgerV3TLSName(stack.Name),
		TLSCAHash:                 tlsCAHash,
		Preview:                   preview,
		Resources:                 resourceRequirements,
		ExtraEnv:                  core.GetDevEnvVars(stack, ledger),
		Monitoring:                monitoringConfiguration,
		Auth:                      authConfiguration,
		ServiceAccountName:        serviceAccountName,
		TopologySpreadConstraints: topologySpreadConstraints,
	})
	desiredSpecMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desiredSpec)
	if err != nil {
		return nil, fmt.Errorf("converting Ledger v3 Cluster spec: %w", err)
	}
	// ResourceRequirements is a value struct in the Ledger API. The
	// unstructured converter therefore emits an empty resources object even
	// though the JSON field is tagged omitempty. Remove it when neither the
	// shared configuration nor Settings provided resources, preserving the
	// historical absence of the field.
	if !hasResourceRequirements(&baseSpec.Resources) && !hasResourceRequirements(resourceRequirements) {
		delete(desiredSpecMap, "resources")
	}

	cluster := newV3Cluster()
	cluster.SetNamespace(stack.Name)
	cluster.SetName(stack.Name)
	_, err = controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), cluster, func() error {
		// Reset the desired spec to the shared configuration on every
		// reconciliation. Stack-specific values below deliberately override it.
		if err := unstructured.SetNestedMap(cluster.Object, desiredSpecMap, "spec"); err != nil {
			return err
		}

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

		return nil
	})
	return cluster, err
}

func ledgerV3BaseSpec(ctx core.Context, stack string) (*ledgerv1alpha1.ClusterSpec, error) {
	stackConfigurations := &v1beta1.LedgerConfigurationList{}
	if err := ctx.GetClient().List(ctx, stackConfigurations, client.MatchingFields{"stack": stack}); err != nil {
		return nil, fmt.Errorf("listing LedgerConfigurations for stack %q: %w", stack, err)
	}

	wildcardConfigurations := &v1beta1.LedgerConfigurationList{}
	if err := ctx.GetClient().List(ctx, wildcardConfigurations, client.MatchingFields{"stack": "*"}); err != nil {
		return nil, fmt.Errorf("listing wildcard LedgerConfigurations: %w", err)
	}

	configurations := append(stackConfigurations.Items, wildcardConfigurations.Items...)
	slices.SortStableFunc(configurations, func(a, b v1beta1.LedgerConfiguration) int {
		switch {
		case a.IsWildcard() && !b.IsWildcard():
			return 1
		case !a.IsWildcard() && b.IsWildcard():
			return -1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})
	if len(configurations) == 0 {
		return &ledgerv1alpha1.ClusterSpec{}, nil
	}

	return configurations[0].Spec.Cluster.DeepCopy(), nil
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
