package resourcereferences

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

// HasResourceReferenceOwner validates ownership against the API server: a stale or fabricated owner reference
// must not turn an independently managed Secret into a registry replica.
func HasResourceReferenceOwner(ctx core.Context, target client.Object) (bool, error) {
	for _, owner := range target.GetOwnerReferences() {
		if owner.APIVersion != v1beta1.GroupVersion.String() || owner.Kind != "ResourceReference" || owner.UID == "" {
			continue
		}
		reference := &v1beta1.ResourceReference{}
		if err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Name: owner.Name}, reference); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return false, fmt.Errorf("checking registry pull secret ownership: %w", err)
			}
			continue
		}
		if reference.UID == owner.UID && reference.Spec.Stack == target.GetNamespace() {
			return true, nil
		}
	}
	return false, nil
}

func requireResourceReferenceOwner(ctx core.Context, target client.Object) error {
	managed, err := HasResourceReferenceOwner(ctx, target)
	if err != nil {
		return err
	}
	if !managed {
		return fmt.Errorf("refusing to overwrite unmanaged registry pull secret %s/%s", target.GetNamespace(), target.GetName())
	}
	return nil
}
