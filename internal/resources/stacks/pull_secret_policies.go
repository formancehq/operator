package stacks

import (
	"context"
	"fmt"
	"slices"

	authorizationv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

// +kubebuilder:rbac:groups=connectivity.formance.com,resources=policies,verbs=get;list;watch;create;update;patch;delete

var pluginPolicyAvailable bool

const instancePullSecretTarget = "ghcr-pull-connectivity-plugins"
const sharedPluginPolicyName = "stack-connectivity-plugin-pull-secrets"
const sharedPluginPolicyMarker = "formance.com/managed-plugin-pull-secret-policy"

func pluginPolicyObject() *unstructured.Unstructured {
	p := &unstructured.Unstructured{}
	p.SetAPIVersion("connectivity.formance.com/v1alpha1")
	p.SetKind("Policy")
	p.SetName(sharedPluginPolicyName)
	return p
}

// A single queue key serializes aggregation independently of Stack copy readiness.
// Watching Settings/Stacks without a primary object also handles the last deletion.
func init() {
	core.Init(func(mgr core.Manager) error {
		ctx := core.NewContext(mgr, context.Background())
		pluginPolicyAvailable = pluginPolicyAPIAvailable(ctx)
		if !pluginPolicyAvailable {
			return nil
		}
		singleton := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, object client.Object) []reconcile.Request {
			if setting, ok := object.(*v1beta1.Settings); ok && !settings.IsConnectivityPullSecretSetting(setting.Spec.Key) {
				return nil
			}
			if policy, ok := object.(*unstructured.Unstructured); ok && policy.GetName() != sharedPluginPolicyName {
				return nil
			}
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: sharedPluginPolicyName}}}
		})
		return ctrl.NewControllerManagedBy(mgr).Named("connectivity-plugin-pull-secret-policy").
			Watches(&v1beta1.Settings{}, singleton).Watches(&v1beta1.Stack{}, singleton).Watches(pluginPolicyObject(), singleton).
			Complete(reconcile.Func(func(ctx context.Context, _ reconcile.Request) (ctrl.Result, error) {
				return ctrl.Result{}, reconcileSharedPluginPolicy(core.NewContext(mgr, ctx))
			}))
	})
}

func pluginPolicyAPIAvailable(ctx core.Context) bool {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Name: "policies.connectivity.formance.com"}, crd); err != nil {
		log.FromContext(ctx).Info("Connectivity plugin Policy API unavailable; continuing without shared policy controller", "error", err)
		return false
	}
	served := false
	for _, version := range crd.Spec.Versions {
		served = served || version.Name == "v1alpha1" && version.Served
	}
	if !served {
		return false
	}
	list := &unstructured.UnstructuredList{}
	list.SetAPIVersion("connectivity.formance.com/v1alpha1")
	list.SetKind("PolicyList")
	if err := ctx.GetAPIReader().List(ctx, list, client.Limit(1)); err != nil {
		log.FromContext(ctx).Info("Connectivity plugin Policies inaccessible", "error", err)
		return false
	}
	for _, verb := range []string{"get", "list", "watch", "create", "update", "patch", "delete"} {
		review := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Group: "connectivity.formance.com", Version: "v1alpha1", Resource: "policies", Verb: verb}}}
		if err := ctx.GetClient().Create(ctx, review); err != nil || !review.Status.Allowed {
			log.FromContext(ctx).Info("Connectivity plugin Policy permission unavailable", "verb", verb, "error", err)
			return false
		}
	}
	return true
}

func requireManagedPluginPolicy(policy *unstructured.Unstructured) error {
	if policy.GetLabels()[sharedPluginPolicyMarker] != "true" || len(policy.GetOwnerReferences()) != 0 {
		return fmt.Errorf("refusing to overwrite unmanaged or foreign-owned plugin Policy %s", policy.GetName())
	}
	return nil
}

func reconcileSharedPluginPolicy(ctx core.Context) error {
	stacks := &v1beta1.StackList{}
	if err := ctx.GetClient().List(ctx, stacks); err != nil {
		return err
	}
	namespaces := []string{}
	for _, stack := range stacks.Items {
		if !stack.DeletionTimestamp.IsZero() {
			continue
		}
		source, err := settings.GetConnectivityInstancePullSecret(ctx, stack.Name)
		if err != nil {
			return err
		}
		if source != "" {
			namespaces = append(namespaces, stack.Name)
		}
	}
	slices.Sort(namespaces)
	policy := pluginPolicyObject()
	if len(namespaces) == 0 {
		if err := ctx.GetAPIReader().Get(ctx, client.ObjectKeyFromObject(policy), policy); err != nil {
			return client.IgnoreNotFound(err)
		}
		if err := requireManagedPluginPolicy(policy); err != nil {
			return err
		}
		uid, rv := policy.GetUID(), policy.GetResourceVersion()
		return client.IgnoreNotFound(ctx.GetClient().Delete(ctx, policy, client.Preconditions{UID: &uid, ResourceVersion: &rv}))
	}
	_, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), policy, func() error {
		if policy.GetResourceVersion() != "" {
			if err := requireManagedPluginPolicy(policy); err != nil {
				return err
			}
		}
		labels := policy.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[sharedPluginPolicyMarker] = "true"
		policy.SetLabels(labels)
		values := make([]any, len(namespaces))
		for i, name := range namespaces {
			values[i] = name
		}
		policy.Object["spec"] = map[string]any{
			"instanceSelector":  map[string]any{},
			"namespaceSelector": map[string]any{"matchExpressions": []any{map[string]any{"key": "kubernetes.io/metadata.name", "operator": "In", "values": values}}},
			"mode":              "Enforce", "priority": int64(0),
			"mutate": map[string]any{"imagePullSecrets": []any{map[string]any{"name": instancePullSecretTarget}}},
		}
		return nil
	})
	return err
}

func requirePluginPolicyCapability(source string) error {
	if source != "" && !pluginPolicyAvailable {
		return core.NewPendingError().WithMessage("Connectivity plugin Policy unavailable; install the Policy CRD, grant policies permissions and restart Stack Operator")
	}
	return nil
}
