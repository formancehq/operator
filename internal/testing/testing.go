package testing

import (
	"context"

	"github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal"
	. "github.com/numary/formance-operator/internal/collectionutil"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ConditionStatus(c client.Client, object internal.Object, conditionType string) func() v1.ConditionStatus {
	return func() v1.ConditionStatus {
		c := Condition(c, object, conditionType)()
		if c == nil {
			return v1.ConditionUnknown
		}
		return c.Status
	}
}

func Condition(c client.Client, object internal.Object, conditionType string) func() *sharedtypes.Condition {
	return func() *sharedtypes.Condition {
		err := c.Get(context.Background(), client.ObjectKeyFromObject(object), object)
		if err != nil {
			return nil
		}
		return First(object.GetConditions(), func(t sharedtypes.Condition) bool {
			return t.Type == conditionType
		})
	}
}

func NotFound(c client.Client, object client.Object) func() bool {
	return func() bool {
		err := c.Get(context.Background(), client.ObjectKeyFromObject(object), object)
		switch {
		case errors.IsNotFound(err):
			return true
		case err != nil:
			panic(err)
		default:
			return false
		}
	}
}

func Exists(c client.Client, object client.Object) func() bool {
	return func() bool {
		return c.Get(context.Background(), client.ObjectKeyFromObject(object), object) == nil
	}
}
