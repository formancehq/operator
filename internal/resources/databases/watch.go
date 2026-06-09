package databases

import (
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func Watch[T client.Object]() core.ReconcilerOption[T] {
	var t T
	t = reflect.New(reflect.TypeOf(t).Elem()).Interface().(T)
	return core.WithWatch[T, *v1beta1.Database](func(ctx core.Context, database *v1beta1.Database) []reconcile.Request {
		serviceName, err := core.LowerCamelCaseKind(ctx, t)
		if err != nil {
			log.FromContext(ctx).Error(err, "resolving object kind, dropping event")
			return []reconcile.Request{}
		}
		if database.Spec.Service != serviceName {
			return []reconcile.Request{}
		}

		slice := reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(t)), 0, 0).Interface()

		err = core.GetAllStackDependencies(ctx, database.Spec.Stack, &slice)
		if err != nil {
			return []reconcile.Request{}
		}

		objects := make([]client.Object, 0)
		for i := 0; i < reflect.ValueOf(slice).Len(); i++ {
			objects = append(objects, reflect.ValueOf(slice).Index(i).Interface().(client.Object))
		}

		return core.MapObjectToReconcileRequests(objects...)
	})
}
