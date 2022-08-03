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

func (f *finalizer) handle(ctx context.Context, client client.Client, ob client.Object, fn func() error) (bool, error) {
	if isDeleted(ob) {
		if !scopeFinalizer.isPresent(ob) {
			return true, nil
		}
		if err := fn(); err != nil {
			return true, err
		}
		if err := scopeFinalizer.removeFinalizer(ctx, client, ob); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

func (f *finalizer) assertIsInstalled(ctx context.Context, client client.Client, ob client.Object) error {
	if !f.isPresent(ob) {
		return scopeFinalizer.add(ctx, client, ob)
	}
	return nil
}

func newFinalizer(name string) *finalizer {
	return &finalizer{
		name: name,
	}
}
