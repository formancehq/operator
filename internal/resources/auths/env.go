package auths

import (
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

type ProtectedAuthConfiguration struct {
	Issuer               string
	Issuers              []string
	ReadKeySetMaxRetries int
	CheckScopes          bool
	Service              string
}

func GetProtectedConfiguration(ctx Context, stack *v1beta1.Stack, moduleName string, auth *v1beta1.AuthConfig) (*ProtectedAuthConfiguration, error) {
	hasAuth, err := HasDependency(ctx, stack.Name, &v1beta1.Auth{})
	if err != nil {
		return nil, err
	}
	if !hasAuth {
		return nil, nil
	}

	url, err := getUrl(ctx, stack.Name)
	if err != nil {
		return nil, err
	}

	issuers, err := settings.GetTrimmedStringSlice(ctx, stack.Name, "auth", "issuers")
	if err != nil {
		return nil, err
	}

	configuration := &ProtectedAuthConfiguration{
		Issuer:  url,
		Issuers: issuers,
		Service: moduleName,
	}
	if auth != nil {
		configuration.ReadKeySetMaxRetries = auth.ReadKeySetMaxRetries
	}

	// Check if scope verification is enabled via Settings or module spec
	configuration.CheckScopes, err = shouldCheckScopes(ctx, stack.Name, moduleName, auth)
	if err != nil {
		return nil, err
	}
	return configuration, nil
}

func ProtectedEnvVars(ctx Context, stack *v1beta1.Stack, moduleName string, auth *v1beta1.AuthConfig) ([]v1.EnvVar, error) {
	configuration, err := GetProtectedConfiguration(ctx, stack, moduleName, auth)
	if err != nil {
		return nil, err
	}
	if configuration == nil {
		return nil, nil
	}

	ret := []v1.EnvVar{
		Env("AUTH_ENABLED", "true"),
		Env("AUTH_ISSUER", configuration.Issuer),
	}
	if len(configuration.Issuers) > 0 {
		ret = append(ret, Env("AUTH_ISSUERS", strings.Join(configuration.Issuers, ",")))
	}
	if configuration.ReadKeySetMaxRetries != 0 {
		ret = append(ret,
			Env("AUTH_READ_KEY_SET_MAX_RETRIES", strconv.Itoa(configuration.ReadKeySetMaxRetries)),
		)
	}

	if configuration.CheckScopes {
		ret = append(ret,
			Env("AUTH_CHECK_SCOPES", "true"),
			Env("AUTH_SERVICE", configuration.Service),
		)
	}

	return ret, nil
}

// shouldCheckScopes determines if scope verification should be enabled for a module.
// Priority order:
// 1. Module spec field: auth.CheckScopes (if auth is not nil and CheckScopes is true)
// 2. Settings with specific module name: auth.<module-name>.check-scopes
// 3. Settings with wildcard: auth.*.check-scopes
// 4. Default: false
func shouldCheckScopes(ctx Context, stackName, moduleName string, auth *v1beta1.AuthConfig) (bool, error) {
	// First, check module spec (highest priority)
	if auth != nil && auth.CheckScopes {
		return true, nil
	}

	// Otherwise, fallback to Settings (supports both specific module and wildcard)
	checkScopesFromSettings, err := settings.GetBool(ctx, stackName, "auth", moduleName, "check-scopes")
	if err != nil {
		return false, err
	}

	// If Settings exists, use it
	if checkScopesFromSettings != nil {
		return *checkScopesFromSettings, nil
	}

	return false, nil
}
