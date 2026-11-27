package connectivities

import (
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
)

// Credentials come only from the Core/API images already resolved by the module.
// Their names remain unchanged in the delegated Connectivity specification.
// Retain references for the Stack lifetime: Ledger/Auth may still consume these
// names after Connectivity is removed or its image configuration changes.
func reconcileImagePullSecretReferences(ctx core.Context, stack *v1beta1.Stack, coreSecrets, apiSecrets []corev1.LocalObjectReference) error {
	desired := map[string]bool{}
	pending := false
	for _, secret := range append(append([]corev1.LocalObjectReference{}, coreSecrets...), apiSecrets...) {
		hash := sha256.Sum256([]byte(string(stack.UID) + "/" + secret.Name))
		name := fmt.Sprintf("%s-connectivity-pull-secret-%x", stack.Name, hash[:12])
		if desired[name] {
			continue
		}
		desired[name] = true
		ref := &v1beta1.ResourceReference{ObjectMeta: metav1.ObjectMeta{Name: name}}
		_, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), ref, func() error {
			if ref.ResourceVersion != "" && !metav1.IsControlledBy(ref, stack) {
				return fmt.Errorf("Connectivity pull secret reference %s has a different owner", name)
			}
			if ref.Labels == nil {
				ref.Labels = map[string]string{}
			}
			ref.Labels[resourcereferences.ManagedPullSecretLabel] = "true"
			ref.Spec.Stack = stack.Name
			ref.Spec.Name = secret.Name
			ref.Spec.TargetName = ""
			ref.Spec.GroupVersionKind = &metav1.GroupVersionKind{Version: "v1", Kind: "Secret"}
			return controllerutil.SetControllerReference(stack, ref, ctx.GetScheme())
		})
		if err != nil {
			return err
		}
		pending = pending || !ref.Status.Ready || ref.Status.ObservedGeneration != ref.Generation
	}
	// Retained names no longer configured do not gate current module readiness.
	if pending {
		return core.NewPendingError().WithMessage("Waiting for Connectivity Core/API pull secret references")
	}
	return nil
}

// Stack-owned copies outlive Connectivity; readiness events still wake its module.
func mapPullSecretReferenceToConnectivity(ctx core.Context, reference *v1beta1.ResourceReference) []reconcile.Request {
	if reference.Spec.Stack == "" {
		return nil
	}
	list := &v1beta1.ConnectivityList{}
	if err := ctx.GetClient().List(ctx, list, client.MatchingFields{"stack": reference.Spec.Stack}); err != nil {
		log.FromContext(ctx).Error(err, "listing Connectivity for pull secret reference watch", "stack", reference.Spec.Stack)
		return nil
	}
	items := make([]*v1beta1.Connectivity, len(list.Items))
	for i := range list.Items {
		items[i] = &list.Items[i]
	}
	return core.MapObjectToReconcileRequests(items...)
}
