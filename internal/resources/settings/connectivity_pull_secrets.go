package settings

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/formancehq/operator/v3/internal/core"
)

const ConnectivityInstancePullSecrets = "connectivity.instances.image-pull-secrets"

// GetConnectivityInstancePullSecret resolves one authorized source Secret name.
func GetConnectivityInstancePullSecret(ctx core.Context, stack string) (string, error) {
	name, err := GetStringOrEmpty(ctx, stack, "connectivity", "instances", "image-pull-secrets")
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name != "" && len(validation.IsDNS1123Subdomain(name)) != 0 {
		return "", fmt.Errorf("%s for stack %s must contain one valid source Secret name, not a list", ConnectivityInstancePullSecrets, stack)
	}
	return name, nil
}

func IsConnectivityPullSecretSetting(key string) bool {
	parts := SplitKeywordWithDot(key)
	for _, candidate := range []string{ConnectivityInstancePullSecrets} {
		expected := SplitKeywordWithDot(candidate)
		if len(parts) != len(expected) {
			continue
		}
		matches := true
		for i := range parts {
			matches = matches && (parts[i] == "*" || parts[i] == expected[i])
		}
		if matches {
			return true
		}
	}
	return false
}
