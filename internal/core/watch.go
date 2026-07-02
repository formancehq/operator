package core

import (
	"context"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func WatchDependents(mgr Manager, t client.Object) func(ctx context.Context, object client.Object) []reconcile.Request {
	return func(ctx context.Context, object client.Object) []reconcile.Request {

		slice := reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(t)), 0, 0).Interface()
		stackName := stackNameFromObject(object)
		if stackName == "" {
			return nil
		}

		err := GetAllStackDependencies(
			NewContext(mgr, ctx),
			stackName, &slice)
		if err != nil {
			return nil
		}

		objects := make([]client.Object, 0)
		for i := 0; i < reflect.ValueOf(slice).Len(); i++ {
			objects = append(objects, reflect.ValueOf(slice).Index(i).Interface().(client.Object))
		}

		return MapObjectToReconcileRequests(objects...)
	}
}

func stackNameFromObject(object client.Object) string {
	if dependent, ok := object.(v1beta1.Dependent); ok && dependent.GetStack() != "" {
		return dependent.GetStack()
	}

	if labels := object.GetLabels(); labels != nil {
		if stackName := labels[v1beta1.StackLabel]; stackName != "" && stackName != "any" {
			return stackName
		}
	}

	if annotations := object.GetAnnotations(); annotations != nil {
		if stackName := annotations[v1beta1.StackLabel]; stackName != "" && stackName != "any" {
			return stackName
		}
	}

	for _, ownerReference := range object.GetOwnerReferences() {
		if ownerReference.APIVersion == v1beta1.GroupVersion.String() && ownerReference.Kind == "Stack" {
			return ownerReference.Name
		}
	}

	return ""
}

func Watch(mgr Manager, t client.Object) func(ctx context.Context, object client.Object) []reconcile.Request {
	return func(ctx context.Context, object client.Object) []reconcile.Request {

		slice := reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(t)), 0, 0).Interface()

		err := GetAllStackDependencies(
			NewContext(mgr, ctx),
			object.(*v1beta1.Stack).Name, &slice)
		if err != nil {
			return nil
		}

		objects := make([]client.Object, 0)
		for i := 0; i < reflect.ValueOf(slice).Len(); i++ {
			objects = append(objects, reflect.ValueOf(slice).Index(i).Interface().(client.Object))
		}

		return MapObjectToReconcileRequests(objects...)
	}
}
