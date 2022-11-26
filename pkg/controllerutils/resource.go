package controllerutils

import (
	"context"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func CreateOrUpdateWithController[T client.Object](ctx context.Context, client client.Client, scheme *runtime.Scheme,
	key types.NamespacedName, owner client.Object, mutate func(t T) error) (T, controllerutil.OperationResult, error) {
	var ret T
	ret = reflect.New(reflect.TypeOf(ret).Elem()).Interface().(T)
	ret.SetNamespace(key.Namespace)
	ret.SetName(key.Name)
	operationResult, err := controllerutil.CreateOrUpdate(ctx, client, ret, func() error {
		err := mutate(ret)
		if err != nil {
			return err
		}
		if !metav1.IsControlledBy(ret, owner) {
			return controllerutil.SetControllerReference(owner, ret, scheme)
		}
		return nil
	})
	return ret, operationResult, err
}
