package stacks

import (
	"crypto/sha256"
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

// +kubebuilder:rbac:groups=connectivity.formance.com,resources=policies,verbs=get;list;watch;create;update;patch;delete

var pullSecretPolicyGVK = schema.GroupVersionKind{Group: "connectivity.formance.com", Version: "v1alpha1", Kind: "Policy"}
var pullSecretPoliciesAvailable bool

// Like other delegated APIs, discover this optional capability at startup.
// Clusters without Connectivity must still reconcile ordinary Stacks.
func withPullSecretPolicyWatch() core.ReconcilerOption[*v1beta1.Stack] {
	return core.WithRaw[*v1beta1.Stack](func(ctx core.Context, b *builder.Builder) error {
		pullSecretPoliciesAvailable = false
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Name: "policies.connectivity.formance.com"}, crd); err != nil {
			log.FromContext(ctx).Info("Connectivity pull secret Policies unavailable", "error", err)
			return nil
		}
		for _, version := range crd.Spec.Versions {
			if version.Name != pullSecretPolicyGVK.Version || !version.Served {
				continue
			}
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(pullSecretPolicyGVK.GroupVersion().WithKind("PolicyList"))
			if err := ctx.GetAPIReader().List(ctx, list, client.Limit(1)); err != nil {
				log.FromContext(ctx).Info("Connectivity pull secret Policies inaccessible", "error", err)
				return nil
			}
			for _, verb := range []string{"get", "list", "watch", "create", "update", "patch", "delete"} {
				review := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Group: pullSecretPolicyGVK.Group, Version: pullSecretPolicyGVK.Version, Resource: "policies", Verb: verb}}}
				if err := ctx.GetClient().Create(ctx, review); err != nil || !review.Status.Allowed {
					log.FromContext(ctx).Info("Connectivity pull secret Policy permission unavailable", "verb", verb, "error", err)
					return nil
				}
			}
			policy := &unstructured.Unstructured{}
			policy.SetGroupVersionKind(pullSecretPolicyGVK)
			b.Owns(policy)
			pullSecretPoliciesAvailable = true
			return nil
		}
		return nil
	})
}

func pullSecretPolicyName(stack *v1beta1.Stack, axis string) string {
	hash := sha256.Sum256([]byte(string(stack.UID)))
	return fmt.Sprintf("stack-pull-secrets-%x-%s", hash[:16], strings.ToLower(axis))
}

func reconcilePullSecretPolicies(ctx core.Context, stack *v1beta1.Stack, connectivity, instances []string) error {
	if !pullSecretPoliciesAvailable {
		if len(connectivity) == 0 && len(instances) == 0 {
			return nil
		}
		return core.NewPendingError().WithMessage("Connectivity pull secret Policies unavailable; install the Policy CRD, grant policies permissions and restart Stack Operator")
	}
	for _, target := range []struct {
		axis  string
		names []string
	}{
		{"connectivitySelector", connectivity}, {"instanceSelector", instances},
	} {
		policy := &unstructured.Unstructured{}
		policy.SetGroupVersionKind(pullSecretPolicyGVK)
		policy.SetName(pullSecretPolicyName(stack, target.axis))
		if len(target.names) == 0 {
			if err := ctx.GetAPIReader().Get(ctx, client.ObjectKeyFromObject(policy), policy); err != nil {
				if client.IgnoreNotFound(err) != nil {
					return err
				}
				continue
			}
			if !metav1.IsControlledBy(policy, stack) {
				continue
			}
			uid := policy.GetUID()
			if err := ctx.GetClient().Delete(ctx, policy, client.Preconditions{UID: &uid}); client.IgnoreNotFound(err) != nil {
				return err
			}
			continue
		}
		_, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), policy, func() error {
			if policy.GetResourceVersion() != "" && !metav1.IsControlledBy(policy, stack) {
				return fmt.Errorf("refusing to overwrite unmanaged Connectivity pull secret Policy %s", policy.GetName())
			}
			secrets := make([]any, 0, len(target.names))
			for _, name := range target.names {
				secrets = append(secrets, map[string]any{"name": name})
			}
			policy.Object["spec"] = map[string]any{
				target.axis:         map[string]any{},
				"namespaceSelector": map[string]any{"matchLabels": map[string]any{"kubernetes.io/metadata.name": stack.Name}},
				"mode":              "Enforce",
				"priority":          int64(0),
				"mutate":            map[string]any{"imagePullSecrets": secrets},
			}
			return controllerutil.SetControllerReference(stack, policy, ctx.GetScheme())
		})
		if err != nil {
			return err
		}
	}
	return nil
}
