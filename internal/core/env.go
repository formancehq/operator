package core

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/formancehq/go-libs/v4/collectionutils"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func Env(name string, value string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  name,
		Value: value,
	}
}

func EnvFromBool(name string, vb bool) corev1.EnvVar {
	value := "true"
	if !vb {
		value = "false"
	}
	return Env(name, value)
}

func EnvFromConfig(name, configName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
				Key: key,
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configName,
				},
			},
		},
	}
}

func EnvFromSecret(name, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				Key: key,
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
			},
		},
	}
}

func EnvVarPlaceholder(key string) string {
	return fmt.Sprintf("$(%s)", key)
}

func ComputeEnvVar(format string, keys ...string) string {
	return fmt.Sprintf(format,
		collectionutils.Map(keys, func(key string) any {
			return EnvVarPlaceholder(key)
		})...,
	)
}

// MergeEnvVars merges overrides into base env vars with deduplication.
// Keys from overrides take precedence. The result is sorted by env var name
// for deterministic output that avoids unnecessary Kubernetes reconciliations.
func MergeEnvVars(base, overrides []corev1.EnvVar) []corev1.EnvVar {
	if len(overrides) == 0 {
		return base
	}

	overrideMap := make(map[string]corev1.EnvVar, len(overrides))
	for _, e := range overrides {
		overrideMap[e.Name] = e
	}

	seen := make(map[string]bool, len(base))
	result := make([]corev1.EnvVar, 0, len(base)+len(overrides))
	for _, e := range base {
		if override, ok := overrideMap[e.Name]; ok {
			result = append(result, override)
		} else {
			result = append(result, e)
		}
		seen[e.Name] = true
	}
	for _, e := range overrides {
		if !seen[e.Name] {
			result = append(result, e)
		}
	}

	slices.SortFunc(result, func(a, b corev1.EnvVar) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})

	return result
}

// TODO: The stack reconciler can create a config map container env var for dev and debug
// This way, we avoid the need to fetch the stack object at each reconciliation loop
func GetDevEnvVars(stack *v1beta1.Stack, service interface {
	IsDebug() bool
	IsDev() bool
}) []corev1.EnvVar {
	return []corev1.EnvVar{
		EnvFromBool("DEBUG", stack.Spec.Debug || service.IsDebug()),
		EnvFromBool("DEV", stack.Spec.Dev || service.IsDev()),
		Env("STACK", stack.Name),
	}
}
