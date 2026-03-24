package core

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

// ErrNoVersionFound is returned when no version can be resolved for a module
// through any of the configured sources (module, stack, or versionsFromFile).
var ErrNoVersionFound = errors.New("no version found")

// MinimumStackVersion is the minimum Stack version the operator supports deploying.
const MinimumStackVersion = "v2.2.0"

// ValidateMinimumVersion checks that a resolved version meets the minimum requirement.
// Non-semver versions (dev tags, SHA refs) are allowed through.
func ValidateMinimumVersion(version string) error {
	if semver.IsValid(version) && semver.Compare(version, MinimumStackVersion) < 0 {
		return fmt.Errorf("version %s is not supported, minimum required: %s - please upgrade your stack", version, MinimumStackVersion)
	}
	return nil
}

func GetModuleVersion(ctx Context, stack *v1beta1.Stack, module v1beta1.Module) (string, error) {
	kinds, _, err := ctx.GetScheme().ObjectKinds(module)
	if err != nil {
		return "", fmt.Errorf("resolving module kind: %w", err)
	}
	kind := strings.ToLower(kinds[0].Kind)

	var version string

	switch {
	case module.GetVersion() != "":
		version = module.GetVersion()
	case stack.Spec.Version != "":
		version = stack.Spec.Version
	case stack.Spec.VersionsFromFile != "":
		versions := &v1beta1.Versions{}
		err := ctx.GetClient().Get(ctx, types.NamespacedName{
			Name: stack.Spec.VersionsFromFile,
		}, versions)
		if client.IgnoreNotFound(err) != nil {
			return "", err
		}
		if err == nil {
			v, ok := versions.Spec[kind]
			if ok && v != "" {
				version = v
			}
		}
		if version == "" {
			return "", fmt.Errorf("%w for module %s on stack %s: module not found in Versions resource %s", ErrNoVersionFound, kind, stack.Name, stack.Spec.VersionsFromFile)
		}
	default:
		return "", fmt.Errorf("%w for module %s on stack %s: stack must define spec.version, spec.versionsFromFile, or the module must define its own version", ErrNoVersionFound, kind, stack.Name)
	}

	if err := ValidateMinimumVersion(version); err != nil {
		return "", err
	}

	return version, nil
}
