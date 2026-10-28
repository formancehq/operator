package resourcereferences

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

// requireResourceReferenceOwner prevents independent sources from racing to
// overwrite one target. Plain references retain legacy unmanaged adoption.
func requireResourceReferenceOwner(ctx core.Context, target client.Object, desired *v1beta1.ResourceReference) error {
	managed := false
	for _, owner := range target.GetOwnerReferences() {
		if owner.APIVersion != v1beta1.GroupVersion.String() || owner.Kind != "ResourceReference" || owner.UID == "" {
			continue
		}
		reference := &v1beta1.ResourceReference{}
		if err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Name: owner.Name}, reference); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return err
			}
			continue
		}
		if reference.UID != owner.UID {
			continue
		}
		if reference.Spec.Stack != target.GetNamespace() {
			return fmt.Errorf("incompatible ResourceReference owner for target %s/%s", target.GetNamespace(), target.GetName())
		}
		managed = true
		// Updating this reference's source is allowed; all other live owners must
		// request exactly the same source and destination (including resource kind).
		if reference.UID == desired.UID {
			continue
		}
		if reference.Spec.Name != desired.Spec.Name || reference.GetTargetName() != desired.GetTargetName() || reference.Spec.GroupVersionKind == nil || desired.Spec.GroupVersionKind == nil || *reference.Spec.GroupVersionKind != *desired.Spec.GroupVersionKind {
			return fmt.Errorf("incompatible ResourceReference source/destination for target %s/%s", target.GetNamespace(), target.GetName())
		}
	}
	if !managed && (desired.Labels[ManagedPullSecretLabel] == "true" || desired.Spec.TargetName != "") {
		return fmt.Errorf("refusing to overwrite unmanaged pull secret %s/%s", target.GetNamespace(), target.GetName())
	}
	return nil
}
