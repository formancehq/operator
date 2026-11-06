package stacks

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
)

// Each usage owns its reference and destination, even when both sources match.
func reconcilePullSecretReferences(ctx core.Context, stack *v1beta1.Stack, instances string) error {
	pending := false
	for _, usage := range []struct{ source, target string }{{instances, instancePullSecretTarget}} {
		reference := &v1beta1.ResourceReference{ObjectMeta: metav1.ObjectMeta{Name: stack.Name + "-" + usage.target}}
		if usage.source == "" {
			if err := ctx.GetAPIReader().Get(ctx, client.ObjectKeyFromObject(reference), reference); err != nil {
				if client.IgnoreNotFound(err) != nil {
					return err
				}
				continue
			}
			if !metav1.IsControlledBy(reference, stack) {
				continue
			}
			uid := reference.UID
			if err := ctx.GetClient().Delete(ctx, reference, client.Preconditions{UID: &uid}); client.IgnoreNotFound(err) != nil {
				return err
			}
			continue
		}

		_, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), reference, func() error {
			if reference.ResourceVersion != "" && !metav1.IsControlledBy(reference, stack) {
				return fmt.Errorf("pull secret reference %s has a different owner", reference.Name)
			}
			if reference.Labels == nil {
				reference.Labels = map[string]string{}
			}
			reference.Labels[resourcereferences.ManagedPullSecretLabel] = "true"
			reference.Spec.Stack = stack.Name
			reference.Spec.Name = usage.source
			reference.Spec.TargetName = usage.target
			reference.Spec.GroupVersionKind = &metav1.GroupVersionKind{Version: "v1", Kind: "Secret"}
			return controllerutil.SetControllerReference(stack, reference, ctx.GetScheme())
		})
		if err != nil {
			return err
		}
		pending = pending || !reference.Status.Ready || reference.Status.ObservedGeneration != reference.Generation
	}
	if pending {
		return core.NewPendingError().WithMessage("Waiting for Connectivity pull secret references")
	}
	return nil
}
