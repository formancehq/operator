package connectivities

import (
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
)

// Credentials come only from the Core/API images already resolved by the module.
// Their names remain unchanged in the delegated Connectivity specification.
func reconcileImagePullSecretReferences(ctx core.Context, stack *v1beta1.Stack, module *v1beta1.Connectivity, coreSecrets, apiSecrets []corev1.LocalObjectReference) error {
	desired := map[string]bool{}
	pending := false
	for _, secret := range append(append([]corev1.LocalObjectReference{}, coreSecrets...), apiSecrets...) {
		hash := sha256.Sum256([]byte(string(module.UID) + "/" + secret.Name))
		name := fmt.Sprintf("%s-connectivity-pull-secret-%x", stack.Name, hash[:12])
		if desired[name] {
			continue
		}
		desired[name] = true
		ref := &v1beta1.ResourceReference{ObjectMeta: metav1.ObjectMeta{Name: name}}
		_, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), ref, func() error {
			if ref.ResourceVersion != "" && !metav1.IsControlledBy(ref, module) {
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
			return controllerutil.SetControllerReference(module, ref, ctx.GetScheme())
		})
		if err != nil {
			return err
		}
		pending = pending || !ref.Status.Ready || ref.Status.ObservedGeneration != ref.Generation
	}
	// Retain previous credentials while any replacement is still unavailable.
	if pending {
		return core.NewPendingError().WithMessage("Waiting for Connectivity Core/API pull secret references")
	}
	list := &v1beta1.ResourceReferenceList{}
	if err := ctx.GetClient().List(ctx, list, client.MatchingLabels{resourcereferences.ManagedPullSecretLabel: "true"}); err != nil {
		return err
	}
	for _, ref := range list.Items {
		if metav1.IsControlledBy(&ref, module) && !desired[ref.Name] {
			uid := ref.UID
			if err := ctx.GetClient().Delete(ctx, &ref, client.Preconditions{UID: &uid}); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}
