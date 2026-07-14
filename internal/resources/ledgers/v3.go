package ledgers

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
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
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

const (
	ledgerV3Threshold             = "v3.0.0-alpha"
	ledgerV3ClusterReadyCondition = "LedgerV3ClusterReady"
)

var (
	ledgerV3ClusterGVK = schema.GroupVersionKind{
		Group:   "ledger.formance.com",
		Version: "v1alpha1",
		Kind:    "Cluster",
	}
	ledgerV3ClusterAvailable bool
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

			for _, crd := range crds.Items {
				if crd.Spec.Group != ledgerV3ClusterGVK.Group || crd.Spec.Names.Kind != ledgerV3ClusterGVK.Kind {
					continue
				}
				for _, version := range crd.Spec.Versions {
					if version.Name == ledgerV3ClusterGVK.Version && version.Served {
						ledgerV3ClusterAvailable = true
						cluster := newV3Cluster()
						options.Owns[cluster] = nil
						b.Owns(cluster)
						log.FromContext(ctx).Info("Ledger v3 Cluster CRD is available", "gvk", ledgerV3ClusterGVK)
						return nil
					}
				}
			}

			log.FromContext(ctx).Info("Ledger v3 Cluster CRD is not available", "gvk", ledgerV3ClusterGVK)
			return nil
		})
	}
}

func reconcileV3(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string) error {
	if !ledgerV3ClusterAvailable {
		setLedgerV3Condition(ledger, metav1.ConditionFalse, "OperatorUnavailable", "Ledger v3 Cluster CRD is not installed")
		return core.NewPendingError().WithMessage("Ledger v3 operator unavailable: Cluster CRD is not installed")
	}

	cluster := newV3Cluster()
	clusterKey := types.NamespacedName{Namespace: stack.Name, Name: stack.Name}
	err := ctx.GetClient().Get(ctx, clusterKey, cluster)
	if apierrors.IsNotFound(err) {
		hasLegacyResources, err := legacyLedgerResourcesExist(ctx, stack)
		if err != nil {
			return err
		}
		if hasLegacyResources {
			setLedgerV3Condition(ledger, metav1.ConditionFalse, "MigrationRequired", "Legacy Ledger resources exist; an explicit v2 to v3 migration is required")
			return core.NewPendingError().WithMessage("migration required before switching Ledger from v2 to v3")
		}
	} else if err != nil {
		return err
	}

	if err := gatewayhttpapis.Create(ctx, ledger, gatewayhttpapis.WithHealthCheckEndpoint("livez")); err != nil {
		return err
	}

	cluster, err = createOrUpdateV3Cluster(ctx, stack, ledger, version)
	if err != nil {
		setLedgerV3Condition(ledger, metav1.ConditionFalse, "ReconcileFailed", err.Error())
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

func v3ClusterExists(ctx core.Context, stack *v1beta1.Stack) (bool, error) {
	cluster := newV3Cluster()
	err := ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
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

func createOrUpdateV3Cluster(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string) (*unstructured.Unstructured, error) {
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

	extraEnv, err := buildV3ExtraEnv(ctx, stack, ledger)
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

func imageRepository(image *registries.ImageConfiguration) string {
	if image.Registry == "" {
		return image.Image
	}
	return strings.TrimSuffix(image.Registry, "/") + "/" + image.Image
}

func buildV3ExtraEnv(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger) ([]any, error) {
	env, err := settings.GetOTELEnvVars(ctx, stack.Name, "ledger", " ")
	if err != nil {
		return nil, err
	}
	authEnv, err := auths.ProtectedEnvVars(ctx, stack, "ledger", ledger.Spec.Auth)
	if err != nil {
		return nil, err
	}
	env = append(env, authEnv...)
	env = append(env, core.GetDevEnvVars(stack, ledger)...)

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
