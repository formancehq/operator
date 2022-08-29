package sharedtypes

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func AddPrefixToFieldError(prefix string) func(t1 *field.Error) *field.Error {
	return func(t1 *field.Error) *field.Error {
		t1.Field = fmt.Sprintf("%s.%s", prefix, t1.Field)
		return t1
	}
}
