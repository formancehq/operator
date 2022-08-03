package finalizerutil

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func isDeleted(meta client.Object) bool {
	return meta.GetDeletionTimestamp() != nil && !meta.GetDeletionTimestamp().IsZero()
}

type Finalizer struct {
	name string
}

func (f *Finalizer) Add(ctx context.Context, client client.Client, object client.Object) error {
	controllerutil.AddFinalizer(object, f.name)
	return client.Update(ctx, object)
}

func (f *Finalizer) Remove(ctx context.Context, client client.Client, object client.Object) error {
	controllerutil.RemoveFinalizer(object, f.name)
	return client.Update(ctx, object)
}

func (f *Finalizer) IsPresent(ob client.Object) bool {
	return controllerutil.ContainsFinalizer(ob, f.name)
}

func (f *Finalizer) Handle(ctx context.Context, client client.Client, ob client.Object, fn func() error) (bool, error) {
	if isDeleted(ob) {
		if !f.IsPresent(ob) {
			return true, nil
		}
		if err := fn(); err != nil {
			return true, err
		}
		if err := f.Remove(ctx, client, ob); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

func (f *Finalizer) AssertIsInstalled(ctx context.Context, client client.Client, ob client.Object) error {
	if !f.IsPresent(ob) {
		return f.Add(ctx, client, ob)
	}
	return nil
}

func New(name string) *Finalizer {
	return &Finalizer{
		name: name,
	}
}
