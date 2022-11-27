package internal

import (
	"context"

	"github.com/formancehq/operator/apis/components/v1beta1"
	pkgError "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func DefineOwner(ctx context.Context, client client.Client, scheme *runtime.Scheme, ob client.Object, reference string) error {
	auth := &v1beta1.Auth{}
	if err := client.Get(ctx, types.NamespacedName{
		Namespace: ob.GetNamespace(),
		Name:      ob.GetNamespace() + "-" + reference,
	}, auth); err != nil {
		return pkgError.Wrap(err, "Retrieving Auth object")
	}

	if err := controllerutil.SetOwnerReference(auth, ob, scheme); err != nil {
		return pkgError.Wrap(err, "Setting owner reference to Auth object")
	}

	references := ob.GetOwnerReferences()
	for ind, ref := range references {
		if ref.UID == auth.UID {
			ref.BlockOwnerDeletion = pointer.Bool(true)
			references[ind] = ref
		}
	}
	ob.SetOwnerReferences(references)
	return nil
}
