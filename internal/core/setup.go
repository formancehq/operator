package core

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/go-libs/v4/collectionutils"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func Setup(mgr ctrl.Manager, platform Platform) error {

	if err := indexStackDependentsObjects(mgr); err != nil {
		return err
	}

	if err := indexSettings(mgr); err != nil {
		return err
	}

	wrappedMgr := NewDefaultManager(mgr, platform)
	for _, initializer := range initializers {
		if err := initializer(wrappedMgr); err != nil {
			return err
		}
	}

	return nil
}

// indexStackDependentsObjects automatically add an index on `stack` property for all stack dependents objects
func indexStackDependentsObjects(mgr ctrl.Manager) error {
	for _, rtype := range mgr.GetScheme().AllKnownTypes() {

		object, ok := reflect.New(rtype).Interface().(client.Object)
		if !ok {
			continue
		}

		_, ok = object.(v1beta1.Dependent)
		if !ok {
			continue
		}

		mgr.GetLogger().Info("Detect stack dependency object, automatically index field", "type", rtype)
		if err := mgr.GetFieldIndexer().
			IndexField(context.Background(), object, "stack", func(object client.Object) []string {
				return []string{object.(v1beta1.Dependent).GetStack()}
			}); err != nil {
			mgr.GetLogger().Error(err, "indexing stack field", "type", rtype)
			return err
		}

		kinds, _, err := mgr.GetScheme().ObjectKinds(object)
		if err != nil {
			return err
		}
		us := &unstructured.Unstructured{}
		us.SetGroupVersionKind(kinds[0])
		if err := mgr.GetFieldIndexer().
			IndexField(context.Background(), us, "stack", func(object client.Object) []string {
				stackName, ok := GetStackNameFromUnstructured(object.(*unstructured.Unstructured))
				if !ok {
					return []string{}
				}
				return []string{stackName}
			}); err != nil {
			mgr.GetLogger().Error(err, "indexing stack field", "type", &unstructured.Unstructured{})
			return err
		}
	}
	return nil
}

func indexSettings(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &v1beta1.Settings{}, "stack", func(object client.Object) []string {
			return object.(*v1beta1.Settings).GetStacks()
		}); err != nil {
		mgr.GetLogger().Error(err, "indexing stack field", "type", &v1beta1.Settings{})
		return err
	}

	kinds, _, err := mgr.GetScheme().ObjectKinds(&v1beta1.Settings{})
	if err != nil {
		return err
	}
	us := &unstructured.Unstructured{}
	us.SetGroupVersionKind(kinds[0])
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), us, "stack", func(object client.Object) []string {
			u := object.(*unstructured.Unstructured)
			spec, ok := GetSpecFromUnstructured(u)
			if !ok {
				return []string{}
			}
			stacks, ok := spec["stacks"].([]any)
			if !ok {
				return []string{}
			}
			return collectionutils.Map(stacks, func(v any) string {
				s, _ := v.(string)
				return s
			})
		}); err != nil {
		mgr.GetLogger().Error(err, "indexing stack field", "type", &unstructured.Unstructured{})
		return err
	}
	return nil
}
