package core

import (
	"reflect"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrNotFound               = errors.New("no configuration found")
	ErrMultipleInstancesFound = errors.New("multiple resources found")
)

// GetAllStackDependencies collects, into the slice pointed to by to, the objects
// of that slice's element type belonging to the given stack, skipping the ones
// already being deleted.
//
// The lookup goes through the typed list registered in the scheme, which the
// cache serves from the same informer controllers watch these objects with. A
// reconciliation triggered by an event on a dependency therefore always observes
// that event. Listing the same objects as unstructured would read a second,
// independently synchronized cache: a reconciliation triggered by the deletion
// of a dependency could still observe it, and, no further event being expected,
// leave the conditions it derives from that read stale.
func GetAllStackDependencies(ctx Context, stackName string, to any) error {
	slice := reflect.Indirect(reflect.ValueOf(to)).Interface()
	objectType := reflect.TypeOf(slice).Elem()

	object, ok := reflect.New(objectType.Elem()).Interface().(client.Object)
	if !ok {
		return errors.Errorf("%s does not implement client.Object", objectType)
	}

	list, err := newObjectList(ctx.GetScheme(), object)
	if err != nil {
		return err
	}

	if err := ctx.GetClient().List(ctx, list, client.MatchingFields{
		"stack": stackName,
	}); err != nil {
		return err
	}

	items, err := meta.ExtractList(list)
	if err != nil {
		return errors.Wrapf(err, "extracting items of %T", list)
	}

	ret := reflect.ValueOf(slice)
	for _, item := range items {
		object, ok := item.(client.Object)
		if !ok {
			return errors.Errorf("%T does not implement client.Object", item)
		}
		if !object.GetDeletionTimestamp().IsZero() {
			continue
		}
		ret = reflect.Append(ret, reflect.ValueOf(object))
	}

	reflect.ValueOf(to).Elem().Set(ret)

	return nil
}

// newObjectList returns an empty list of the scheme kind matching object.
func newObjectList(scheme *runtime.Scheme, object client.Object) (client.ObjectList, error) {
	kinds, _, err := scheme.ObjectKinds(object)
	if err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		return nil, errors.Errorf("no kind registered for %T", object)
	}

	listGVK := kinds[0]
	listGVK.Kind += "List"
	listObject, err := scheme.New(listGVK)
	if err != nil {
		return nil, errors.Wrapf(err, "creating %s", listGVK)
	}
	list, ok := listObject.(client.ObjectList)
	if !ok {
		return nil, errors.Errorf("%s does not implement client.ObjectList", listGVK)
	}

	return list, nil
}

func GetSingleDependency(ctx Context, stackName string, to client.Object) error {

	slice := reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(to)), 0, 0).Interface()
	err := GetAllStackDependencies(ctx, stackName, &slice)
	if err != nil {
		return err
	}

	switch reflect.ValueOf(slice).Len() {
	case 0:
		return ErrNotFound
	case 1:
		reflect.Indirect(reflect.ValueOf(to)).Set(reflect.ValueOf(slice).Index(0).Elem())
		return nil
	default:
		return ErrMultipleInstancesFound
	}
}

func HasDependency(ctx Context, stackName string, to client.Object) (bool, error) {
	err := GetSingleDependency(ctx, stackName, to)
	if err != nil && !errors.Is(err, ErrMultipleInstancesFound) {
		switch {
		default:
			return false, err
		case errors.Is(err, ErrNotFound):
			return false, nil
		}
	}
	return true, nil
}

func GetIfExists(ctx Context, stackName string, to client.Object) (bool, error) {
	err := GetSingleDependency(ctx, stackName, to)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return true, nil
}
