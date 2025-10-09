package auths

import (
	"fmt"
	"strconv"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	v1 "k8s.io/api/core/v1"
)

func ProtectedEnvVars(ctx Context, stack *v1beta1.Stack, moduleName string, auth *v1beta1.AuthConfig) ([]v1.EnvVar, error) {
	return ProtectedAPIEnvVarsWithPrefix(ctx, stack, moduleName, auth, "")
}

func ProtectedAPIEnvVarsWithPrefix(ctx Context, stack *v1beta1.Stack, moduleName string, auth *v1beta1.AuthConfig, prefix string) ([]v1.EnvVar, error) {
	ret := make([]v1.EnvVar, 0)

	hasAuth, err := HasDependency(ctx, stack.Name, &v1beta1.Auth{})
	if err != nil {
		return nil, err
	}
	if !hasAuth {
		return ret, nil
	}

	url, err := getUrl(ctx, stack.Name)
	if err != nil {
		return nil, err
	}

	ret = append(ret,
		Env(fmt.Sprintf("%sAUTH_ENABLED", prefix), "true"),
		Env(fmt.Sprintf("%sAUTH_ISSUER", prefix), url),
	)

	if auth != nil {
		if auth.ReadKeySetMaxRetries != 0 {
			ret = append(ret,
				Env(fmt.Sprintf("%sAUTH_READ_KEY_SET_MAX_RETRIES", prefix), strconv.Itoa(auth.ReadKeySetMaxRetries)),
			)
		}
	}

	// Check if scope verification is enabled via Settings or module spec
	checkScopes, err := shouldCheckScopes(ctx, stack.Name, moduleName, auth)
	if err != nil {
		return nil, err
	}

	if checkScopes {
		ret = append(ret,
			Env(fmt.Sprintf("%sAUTH_CHECK_SCOPES", prefix), "true"),
			Env(fmt.Sprintf("%sAUTH_SERVICE", prefix), moduleName),
		)
	}

	return ret, nil
}

// shouldCheckScopes determines if scope verification should be enabled for a module.
// Priority order:
// 1. Settings with specific module name: auth.<module-name>.check-scopes
// 2. Settings with wildcard: auth.*.check-scopes
// 3. Module spec field: auth.CheckScopes
// 4. Default: false
func shouldCheckScopes(ctx Context, stackName, moduleName string, auth *v1beta1.AuthConfig) (bool, error) {
	// Check Settings first (supports both specific module and wildcard)
	checkScopesFromSettings, err := settings.GetBool(ctx, stackName, "auth", moduleName, "check-scopes")
	if err != nil {
		return false, err
	}

	// If Settings exists, use it
	if checkScopesFromSettings != nil {
		return *checkScopesFromSettings, nil
	}

	// Otherwise, fallback to module spec field
	if auth != nil && auth.CheckScopes {
		return true, nil
	}

	return false, nil
}
