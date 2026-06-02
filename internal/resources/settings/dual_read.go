package settings

import (
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/formancehq/operator/v3/internal/core"
)

// Dual-read helpers preferring CRD spec values over Settings.
//
// The Settings system historically held module-specific tuning knobs (ledger
// experimental flags, payments worker tunings, gateway configuration, …).
// These have been moved to typed fields on the corresponding module CRDs.
// To allow a smooth migration, the PreferSpec* helpers below first inspect
// the spec value; only when it is zero do they fall back to a Settings
// lookup and emit a deprecation warning.

func LogDeprecation(ctx core.Context, stack, replacement string, keys ...string) {
	log.FromContext(ctx).Info(
		"DEPRECATION: settings key is deprecated, move the value to the module spec",
		"stack", stack,
		"key", strings.Join(keys, "."),
		"replacement", replacement,
	)
}

// PreferSpecBool returns the dereferenced spec value when non-nil. Otherwise
// it falls back to the boolean setting at keys, returning false if unset.
// replacement is the CRD spec field path quoted in the deprecation log
// (e.g. "Ledger.Spec.ExperimentalFeatures").
func PreferSpecBool(ctx core.Context, stack string, specVal *bool, replacement string, keys ...string) (bool, error) {
	if specVal != nil {
		return *specVal, nil
	}
	value, err := GetBool(ctx, stack, keys...)
	if err != nil {
		return false, err
	}
	if value == nil {
		return false, nil
	}
	LogDeprecation(ctx, stack, replacement, keys...)
	return *value, nil
}

// PreferSpecBoolOrDefault is the variant returning a custom default when
// neither the spec nor the setting is set.
func PreferSpecBoolOrDefault(ctx core.Context, stack string, specVal *bool, defaultValue bool, replacement string, keys ...string) (bool, error) {
	if specVal != nil {
		return *specVal, nil
	}
	value, err := GetBool(ctx, stack, keys...)
	if err != nil {
		return false, err
	}
	if value == nil {
		return defaultValue, nil
	}
	LogDeprecation(ctx, stack, replacement, keys...)
	return *value, nil
}

// PreferSpecInt returns the dereferenced spec value when non-nil. Otherwise
// it falls back to the int setting at keys (returns nil if unset).
func PreferSpecInt(ctx core.Context, stack string, specVal *int, replacement string, keys ...string) (*int, error) {
	if specVal != nil {
		return specVal, nil
	}
	value, err := GetInt(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	LogDeprecation(ctx, stack, replacement, keys...)
	return value, nil
}

// PreferSpecIntOrDefault is the variant returning a custom default when
// neither the spec nor the setting is set.
func PreferSpecIntOrDefault(ctx core.Context, stack string, specVal *int, defaultValue int, replacement string, keys ...string) (int, error) {
	if specVal != nil {
		return *specVal, nil
	}
	value, err := GetInt(ctx, stack, keys...)
	if err != nil {
		return 0, err
	}
	if value == nil {
		return defaultValue, nil
	}
	LogDeprecation(ctx, stack, replacement, keys...)
	return *value, nil
}

// PreferSpecString returns the spec value when non-empty. Otherwise it falls
// back to the string setting at keys, returning the empty string if unset.
func PreferSpecString(ctx core.Context, stack string, specVal string, replacement string, keys ...string) (string, error) {
	if specVal != "" {
		return specVal, nil
	}
	value, err := GetString(ctx, stack, keys...)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", nil
	}
	LogDeprecation(ctx, stack, replacement, keys...)
	return *value, nil
}

// PreferSpecStringSlice returns the spec value when non-empty. Otherwise it
// falls back to the comma-separated string setting at keys.
func PreferSpecStringSlice(ctx core.Context, stack string, specVal []string, replacement string, keys ...string) ([]string, error) {
	if len(specVal) > 0 {
		return specVal, nil
	}
	value, err := GetStringSlice(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	LogDeprecation(ctx, stack, replacement, keys...)
	return value, nil
}

// PreferSpecDuration returns the spec value when non-zero. Otherwise it falls
// back to the duration setting at keys, returning the default when unset.
func PreferSpecDuration(ctx core.Context, stack string, specVal time.Duration, defaultValue time.Duration, replacement string, keys ...string) (time.Duration, error) {
	if specVal != 0 {
		return specVal, nil
	}
	value, err := GetDuration(ctx, stack, keys...)
	if err != nil {
		return 0, err
	}
	if value == nil {
		return defaultValue, nil
	}
	LogDeprecation(ctx, stack, replacement, keys...)
	return *value, nil
}

// PreferSpecMap returns the spec value when non-empty. Otherwise it falls
// back to the comma-separated key=value setting at keys.
func PreferSpecMap(ctx core.Context, stack string, specVal map[string]string, replacement string, keys ...string) (map[string]string, error) {
	if len(specVal) > 0 {
		return specVal, nil
	}
	value, err := GetMap(ctx, stack, keys...)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	LogDeprecation(ctx, stack, replacement, keys...)
	return value, nil
}
