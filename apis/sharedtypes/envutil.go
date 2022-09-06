package sharedtypes

import (
	"fmt"

	"github.com/numary/formance-operator/internal/collectionutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func EnvFromWithPrefix(prefix, key string, value *corev1.EnvVarSource) corev1.EnvVar {
	return corev1.EnvVar{
		Name:      prefix + key,
		ValueFrom: value,
	}
}

func EnvFrom(key string, value *corev1.EnvVarSource) corev1.EnvVar {
	return corev1.EnvVar{
		Name:      key,
		ValueFrom: value,
	}
}

func EnvWithPrefix(prefix, key, value string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  prefix + key,
		Value: value,
	}
}

func Env(key, value string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  key,
		Value: value,
	}
}

func ValidateRequiredConfigValueOrReferenceOnly[T comparable, SRC interface {
	*ConfigSource | *corev1.EnvVarSource
}](key string, v T, source SRC) field.ErrorList {
	var zeroValue T
	ret := field.ErrorList{}
	if !(v == zeroValue || source == nil) {
		ret = append(ret, &field.Error{
			Type:     field.ErrorTypeDuplicate,
			Field:    key,
			BadValue: v,
			Detail:   fmt.Sprintf("Only '%s' OR '%sFrom' can be specified", key, key),
		})
	}
	return ret
}

func ValidateRequiredConfigValueOrReference[T comparable, SRC interface {
	*ConfigSource | *corev1.EnvVarSource
}](key string, v T, source SRC) field.ErrorList {
	var zeroValue T
	ret := field.ErrorList{}
	if v == zeroValue && source == nil {
		ret = append(ret, field.Invalid(
			field.NewPath(key),
			nil,
			fmt.Sprintf("Either '%s' or '%sFrom' must be specified", key, key),
		))
	}
	return append(ret, ValidateRequiredConfigValueOrReferenceOnly(key, v, source)...)
}

func SelectRequiredConfigValueOrReference[VALUE interface {
	string |
	int | int8 | int16 | int32 | int64 |
	uint | uint8 | uint16 | uint32 | uint64
}, SRC interface {
	*ConfigSource | *corev1.EnvVarSource
}](key, prefix string, v VALUE, src SRC) corev1.EnvVar {
	var (
		ret         corev1.EnvVar
		stringValue *string
	)
	switch v := any(v).(type) {
	case string:
		if v != "" {
			stringValue = &v
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		if v != 0 {
			value := fmt.Sprintf("%d", v)
			stringValue = &value
		}
	}
	if stringValue != nil {
		ret = EnvWithPrefix(prefix, key, *stringValue)
	} else {
		switch src := any(src).(type) {
		case *ConfigSource:
			ret = EnvFromWithPrefix(prefix, key, src.Env())
		case *corev1.EnvVarSource:
			ret = corev1.EnvVar{
				Name:      prefix + key,
				ValueFrom: src,
			}
		}
	}
	return ret
}

func EnvVarPlaceholder(key, prefix string) string {
	return fmt.Sprintf("$(%s%s)", prefix, key)
}

func ComputeEnvVar(prefix, format string, keys ...string) string {
	return fmt.Sprintf(format,
		collectionutil.Map(keys, func(key string) any {
			return EnvVarPlaceholder(key, prefix)
		})...,
	)
}
