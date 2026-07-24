package core

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func ListEventPublishers(ctx Context, stackName string) ([]unstructured.Unstructured, error) {
	ret := make([]unstructured.Unstructured, 0)
	var stack *v1beta1.Stack
	for gvk, rtype := range ctx.GetScheme().AllKnownTypes() {
		object, ok := reflect.New(rtype).Interface().(client.Object)
		if !ok {
			continue
		}

		if _, ok := object.(v1beta1.EventPublisher); ok {
			_, isVersionedPublisher := object.(v1beta1.VersionedEventPublisher)
			us := &unstructured.UnstructuredList{}
			us.SetGroupVersionKind(gvk)

			if err := ctx.GetClient().List(ctx, us, client.MatchingFields{
				"stack": stackName,
			}); err != nil {
				return nil, err
			}

			for _, item := range us.Items {
				item.SetGroupVersionKind(gvk)

				if isVersionedPublisher {
					typedObject := reflect.New(rtype).Interface().(client.Object)
					if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, typedObject); err != nil {
						return nil, err
					}
					versionedPublisher := typedObject.(v1beta1.VersionedEventPublisher)

					if stack == nil {
						stack = &v1beta1.Stack{}
						if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: stackName}, stack); err != nil {
							return nil, err
						}
					}

					version, err := GetModuleVersion(ctx, stack, versionedPublisher)
					if err != nil {
						return nil, err
					}
					if !versionedPublisher.PublishesEvents(version) {
						continue
					}
				}

				ret = append(ret, item)
			}
		}
	}

	return ret, nil
}
