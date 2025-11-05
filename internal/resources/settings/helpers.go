package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/formancehq/go-libs/v2/pointer"
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrResourceNotFound         = errors.New("resource not found")
	ErrOptionalResourceNotFound = errors.New("optional resource not found")
)

func Get(ctx core.Context, stack string, keys ...string) (*string, error) {
	allSettingsTargetingStack := &v1beta1.SettingsList{}
	if err := ctx.GetClient().List(ctx, allSettingsTargetingStack, client.MatchingFields{
		"stack":  stack,
		"keylen": fmt.Sprint(len(keys)),
	}); err != nil {
		return nil, fmt.Errorf("listings settings: %w", err)
	}

	allSettingsTargetingAllStacks := &v1beta1.SettingsList{}
	if err := ctx.GetClient().List(ctx, allSettingsTargetingAllStacks, client.MatchingFields{
		"stack":  "*",
		"keylen": fmt.Sprint(len(keys)),
	}); err != nil {
		return nil, fmt.Errorf("listings settings: %w", err)
	}

	return findMatchingSettings(ctx, stack, append(allSettingsTargetingStack.Items, allSettingsTargetingAllStacks.Items...), keys...)
}

func GetString(ctx core.Context, stack string, keys ...string) (*string, error) {
	return Get(ctx, stack, keys...)
}

func GetStringOrDefault(ctx core.Context, stack, defaultValue string, keys ...string) (string, error) {
	value, err := GetString(ctx, stack, keys...)
	if err != nil {
		return "", err
	}
	if value == nil {
		return defaultValue, nil
	}
	return *value, nil
}

func GetStringOrEmpty(ctx core.Context, stack string, keys ...string) (string, error) {
	return GetStringOrDefault(ctx, stack, "", keys...)
}

func GetStringSlice(ctx core.Context, stack string, keys ...string) ([]string, error) {
	value, err := GetString(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, err
	}
	return strings.Split(*value, ","), nil
}

func RequireString(ctx core.Context, stack string, keys ...string) (string, error) {
	value, err := GetString(ctx, stack, keys...)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", core.NewMissingSettingsError(fmt.Sprintf("settings '%s' not found for stack '%s'", strings.Join(keys, "."), stack))
	}
	return *value, nil
}

func GetURL(ctx core.Context, stack string, keys ...string) (*v1beta1.URI, error) {
	value, err := GetString(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	return v1beta1.ParseURL(*value)
}

func RequireURL(ctx core.Context, stack string, keys ...string) (*v1beta1.URI, error) {
	value, err := RequireString(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	return v1beta1.ParseURL(value)
}

func GetInt64(ctx core.Context, stack string, keys ...string) (*int64, error) {
	value, err := Get(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	intValue, err := strconv.ParseInt(*value, 10, 64)
	if err != nil {
		return nil, err
	}

	return &intValue, nil
}

func GetInt32(ctx core.Context, stack string, keys ...string) (*int32, error) {
	value, err := Get(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	intValue, err := strconv.ParseInt(*value, 10, 32)
	if err != nil {
		return nil, err
	}

	return pointer.For(int32(intValue)), nil
}

func GetUInt64(ctx core.Context, stack string, keys ...string) (*uint64, error) {
	value, err := Get(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	intValue, err := strconv.ParseUint(*value, 10, 64)
	if err != nil {
		return nil, err
	}

	return &intValue, nil
}

func GetUInt16(ctx core.Context, stack string, keys ...string) (*uint16, error) {
	value, err := Get(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	intValue, err := strconv.ParseUint(*value, 10, 16)
	if err != nil {
		return nil, err
	}

	return pointer.For(uint16(intValue)), nil
}

func GetInt(ctx core.Context, stack string, keys ...string) (*int, error) {
	value, err := GetInt64(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	return pointer.For(int(*value)), nil
}

func GetUInt(ctx core.Context, stack string, keys ...string) (*uint, error) {
	value, err := GetUInt64(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	return pointer.For(uint(*value)), nil
}

func GetIntOrDefault(ctx core.Context, stack string, defaultValue int, keys ...string) (int, error) {
	value, err := GetInt(ctx, stack, keys...)
	if err != nil {
		return 0, err
	}

	if value == nil {
		return defaultValue, nil
	}
	return *value, nil
}

func GetUInt16OrDefault(ctx core.Context, stack string, defaultValue uint16, keys ...string) (uint16, error) {
	value, err := GetUInt16(ctx, stack, keys...)
	if err != nil {
		return 0, err
	}

	if value == nil {
		return defaultValue, nil
	}
	return *value, nil
}

func GetInt32OrDefault(ctx core.Context, stack string, defaultValue int32, keys ...string) (int32, error) {
	value, err := GetInt32(ctx, stack, keys...)
	if err != nil {
		return 0, err
	}

	if value == nil {
		return defaultValue, nil
	}
	return *value, nil
}

func GetBool(ctx core.Context, stack string, keys ...string) (*bool, error) {
	value, err := Get(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	return pointer.For(*value == "true"), nil
}

func GetBoolOrDefault(ctx core.Context, stack string, defaultValue bool, keys ...string) (bool, error) {
	value, err := GetBool(ctx, stack, keys...)
	if err != nil {
		return false, err
	}
	if value == nil {
		return defaultValue, nil
	}
	return *value, nil
}

func GetBoolOrFalse(ctx core.Context, stack string, keys ...string) (bool, error) {
	return GetBoolOrDefault(ctx, stack, false, keys...)
}

func GetBoolOrTrue(ctx core.Context, stack string, keys ...string) (bool, error) {
	return GetBoolOrDefault(ctx, stack, true, keys...)
}

func GetMap(ctx core.Context, stack string, keys ...string) (map[string]string, error) {
	value, err := GetString(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}

	return parseKeyValueList(*value)
}

func parseKeyValueList(value string) (map[string]string, error) {
	ret := make(map[string]string)

	buf := bytes.NewBufferString(value)
	var (
		isParsingKey = true
		hasQuotes    = false
		parsedKey    string
		parsedValue  string
	)
	for {
		v, err := buf.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if isParsingKey && parsedKey == "" {
					break
				}
				if isParsingKey {
					return nil, fmt.Errorf("invalid key value list, unexpected end of input while parsing key: '%s'", value)
				}
				if hasQuotes {
					return nil, fmt.Errorf("invalid key value list, unexpected end of input while parsing quoted value: '%s'", value)
				}
				ret[strings.TrimSpace(parsedKey)] = strings.TrimSpace(parsedValue)
				break
			}
			return nil, err
		}
		switch v {
		case '=':
			isParsingKey = false
			v, err := buf.ReadByte()
			if err != nil {
				return nil, err
			}
			if v == '"' {
				hasQuotes = true
			} else {
				parsedValue += string(v)
			}
		case '"':
			if isParsingKey {
				return nil, fmt.Errorf("invalid key value list: quotes are not allowed in keys")
			}
			if hasQuotes {
				hasQuotes = false
				isParsingKey = true
				ret[strings.TrimSpace(parsedKey)] = strings.TrimSpace(parsedValue)
				parsedKey = ""
				parsedValue = ""
				if buf.Len() > 0 {
					v, err := buf.ReadByte()
					if err != nil {
						return nil, err
					}
					if v != ',' {
						return nil, fmt.Errorf("invalid key value list, expected comma after quoted value: '%s'", value)
					}
				}
			} else {
				parsedValue += string(v)
			}
		case ',':

			if hasQuotes {
				parsedValue += string(v)
			} else {
				ret[strings.TrimSpace(parsedKey)] = strings.TrimSpace(parsedValue)
				isParsingKey = true
				parsedKey = ""
				parsedValue = ""
			}
		default:
			if isParsingKey {
				parsedKey += string(v)
			} else {
				parsedValue += string(v)
			}
		}
	}

	return ret, nil
}

// TODO(gfyrag): GetAs only allow to map to structure containing only strings.
// With a bit of reflection, we could be able to have a more smart mapping to structure with usefull types.
func GetAs[T any](ctx core.Context, stack string, keys ...string) (*T, error) {
	m, err := GetMap(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}

	var ret T
	ret = reflect.New(reflect.TypeOf(ret)).Elem().Interface().(T)
	if m == nil {
		return &ret, nil
	}

	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}

	if err := json.Unmarshal(data, &ret); err != nil {
		return nil, err
	}

	return &ret, nil
}

func GetMapOrEmpty(ctx core.Context, stack string, keys ...string) (map[string]string, error) {
	value, err := GetMap(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return map[string]string{}, nil
	}

	return value, nil
}

func findMatchingSettings(ctx core.Context, stack string, settings []v1beta1.Settings, flattenKeys ...string) (*string, error) {
	// Keys can be passed as "a.b.c", instead of "a", "b", "c"
	// Keys can be passed as "a.b.*", instead of "a", "b", "*"
	// Keys can be passed as "a.*.c", instead of "a", "*", "c"
	// Keys can be passed as "a."*.*".c," instead of "a", "b", "c"
	slices.SortFunc(settings, sortSettingsByPriority)

	for _, setting := range settings {
		if matchSetting(setting, flattenKeys...) {
			// Value takes precedence over ValueFrom (consistent with Kubernetes EnvVar behavior)
			if setting.Spec.Value != "" {
				return &setting.Spec.Value, nil
			}
			if setting.Spec.ValueFrom != nil {
				// Resolve value from secret or configmap
				value, err := resolveValueFrom(ctx, stack, setting.Spec.ValueFrom)
				if err != nil {
					if errors.Is(err, ErrResourceNotFound) || errors.Is(err, ErrOptionalResourceNotFound) {
						// Try fallback to formance-system namespace
						value, err2 := resolveValueFrom(ctx, "formance-system", setting.Spec.ValueFrom)
						if err2 == nil {
							return &value, nil
						}

						if errors.Is(err, ErrOptionalResourceNotFound) {
							// Optional resource not found, return empty string
							empty := ""
							return &empty, nil
						}
						err = fmt.Errorf("%w: %w", err, err2)
					}
					return nil, fmt.Errorf("resolving valueFrom for setting '%s': %w", setting.Name, err)
				}
				return &value, nil
			}
			// Both Value and ValueFrom are empty, skip this setting
			continue
		}
	}

	return nil, nil
}

func resolveValueFrom(ctx core.Context, namespace string, valueFrom *v1beta1.ValueFrom) (string, error) {
	if valueFrom == nil {
		return "", errors.New("valueFrom is nil")
	}

	if valueFrom.SecretKeyRef != nil {
		secret := &corev1.Secret{}
		secretName := valueFrom.SecretKeyRef.Name
		key := valueFrom.SecretKeyRef.Key
		tSec := types.NamespacedName{
			Namespace: namespace,
			Name:      secretName,
		}

		err := ctx.GetClient().Get(ctx, tSec, secret)
		if err != nil {
			if valueFrom.SecretKeyRef.Optional != nil && *valueFrom.SecretKeyRef.Optional && client.IgnoreNotFound(err) == nil {
				return "", ErrOptionalResourceNotFound
			}
			return "", fmt.Errorf("%w: namespace '%s': %w", ErrResourceNotFound, namespace, err)
		}

		value, ok := secret.Data[key]
		if !ok {
			if valueFrom.SecretKeyRef.Optional != nil && *valueFrom.SecretKeyRef.Optional {
				return "", ErrOptionalResourceNotFound
			}
			return "", fmt.Errorf("key '%s' not found in secret '%s/%s'", key, namespace, secretName)
		}

		return string(value), nil
	}

	if valueFrom.ConfigMapKeyRef != nil {
		configMap := &corev1.ConfigMap{}
		configMapName := valueFrom.ConfigMapKeyRef.Name
		key := valueFrom.ConfigMapKeyRef.Key
		tConf := types.NamespacedName{
			Namespace: namespace,
			Name:      configMapName,
		}

		err := ctx.GetClient().Get(ctx, tConf, configMap)
		if err != nil {
			if valueFrom.ConfigMapKeyRef.Optional != nil && *valueFrom.ConfigMapKeyRef.Optional && client.IgnoreNotFound(err) == nil {
				return "", ErrOptionalResourceNotFound
			}
			return "", fmt.Errorf("%w: namespace '%s': %w", ErrResourceNotFound, namespace, err)
		}

		value, ok := configMap.Data[key]
		if !ok {
			// Try binary data as well
			valueBytes, ok := configMap.BinaryData[key]
			if ok {
				return string(valueBytes), nil
			}
			if valueFrom.ConfigMapKeyRef.Optional != nil && *valueFrom.ConfigMapKeyRef.Optional {
				return "", ErrOptionalResourceNotFound
			}
			return "", fmt.Errorf("key '%s' not found in configmap '%s/%s'", key, namespace, configMapName)
		}

		return value, nil
	}

	return "", errors.New("valueFrom must specify either secretKeyRef or configMapKeyRef")
}

func matchSetting(setting v1beta1.Settings, keys ...string) bool {
	settingKeyParts := SplitKeywordWithDot(setting.Spec.Key)
	for i, settingKeyPart := range settingKeyParts {
		if settingKeyPart == "*" {
			continue
		}
		if settingKeyPart != keys[i] {
			return false
		}
	}
	return true
}

func SplitKeywordWithDot(key string) []string {
	segments := ""
	needQuote := false
	for _, v := range key {
		switch v {
		case '"':
			needQuote = !needQuote
		case '.':
			if !needQuote {
				segments += " "
				continue
			}
			segments += string(v)
		default:
			segments += string(v)
		}
	}

	return strings.Split(segments, " ")
}

func sortSettingsByPriority(a, b v1beta1.Settings) int {
	switch {
	case a.IsWildcard() && !b.IsWildcard():
		return 1
	case !a.IsWildcard() && b.IsWildcard():
		return -1
	}
	aKeys := SplitKeywordWithDot(a.Spec.Key)
	bKeys := SplitKeywordWithDot(b.Spec.Key)

	for i := 0; i < len(aKeys); i++ {
		if aKeys[i] == bKeys[i] {
			continue
		}
		if aKeys[i] == "*" {
			return 1
		}
		if bKeys[i] == "*" {
			return -1
		}
	}

	return 0
}

func IsTrue(v string) bool {
	return strings.ToLower(v) == "true" || v == "1"
}
