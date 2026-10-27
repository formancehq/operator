package settings

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/formancehq/operator/v3/internal/core"
)

const (
	ConnectivityPullSecrets         = "connectivity.image-pull-secrets"
	ConnectivityInstancePullSecrets = "connectivity.instances.image-pull-secrets"
)

// GetConnectivityPullSecrets resolves each target independently, including an
// explicit empty override of a wildcard setting.
func GetConnectivityPullSecrets(ctx core.Context, stack string) (connectivity, instances []string, err error) {
	connectivity, err = GetTrimmedStringSlice(ctx, stack, "connectivity", "image-pull-secrets")
	if err != nil {
		return nil, nil, err
	}
	instances, err = GetTrimmedStringSlice(ctx, stack, "connectivity", "instances", "image-pull-secrets")
	if err != nil {
		return nil, nil, err
	}
	for _, target := range []struct {
		key    string
		output *[]string
	}{
		{ConnectivityPullSecrets, &connectivity}, {ConnectivityInstancePullSecrets, &instances},
	} {
		for _, name := range *target.output {
			if len(validation.IsDNS1123Subdomain(name)) != 0 {
				return nil, nil, fmt.Errorf("invalid Secret name in %s for stack %s", target.key, stack)
			}
		}
		slices.Sort(*target.output)
		*target.output = slices.Compact(*target.output)
	}
	return
}

func IsConnectivityPullSecretSetting(key string) bool {
	parts := SplitKeywordWithDot(key)
	for _, candidate := range []string{ConnectivityPullSecrets, ConnectivityInstancePullSecrets} {
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
