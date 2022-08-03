package authcomponents

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type finalizer struct {
	name string
}

func (f *finalizer) add(ctx context.Context, client client.Client, object client.Object) error {
	controllerutil.AddFinalizer(object, f.name)
	return client.Update(ctx, object)
}

func (f *finalizer) removeFinalizer(ctx context.Context, client client.Client, object client.Object) error {
	controllerutil.RemoveFinalizer(object, f.name)
	return client.Update(ctx, object)
}

func (f *finalizer) isPresent(ob client.Object) bool {
	return controllerutil.ContainsFinalizer(ob, f.name)
}

func newFinalizer(name string) *finalizer {
	return &finalizer{
		name: name,
	}
}
