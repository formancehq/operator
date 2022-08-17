package testing

import (
	"github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ConditionStatus(object sharedtypes.Object, conditionType string) func() v1.ConditionStatus {
	return func() v1.ConditionStatus {
		c := GetCondition(object, conditionType)()
		if c == nil {
			return v1.ConditionUnknown
		}
		return c.Status
	}
}

func GetCondition(object sharedtypes.Object, conditionType string) func() *sharedtypes.Condition {
	return func() *sharedtypes.Condition {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil {
			return nil
		}
		return First(*object.GetConditions(), func(t sharedtypes.Condition) bool {
			return t.Type == conditionType
		})
	}
}

func NotFound(object client.Object) func() bool {
	return func() bool {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(object), object)
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

func Exists(object client.Object) func() bool {
	return func() bool {
		return k8sClient.Get(ctx, client.ObjectKeyFromObject(object), object) == nil
	}
}
