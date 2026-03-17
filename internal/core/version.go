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

func GetModuleVersion(ctx Context, stack *v1beta1.Stack, module v1beta1.Module) (string, error) {
	kinds, _, err := ctx.GetScheme().ObjectKinds(module)
	if err != nil {
		return "", fmt.Errorf("resolving module kind: %w", err)
	}
	kind := strings.ToLower(kinds[0].Kind)

	if module.GetVersion() != "" {
		return module.GetVersion(), nil
	}
	if stack.Spec.Version != "" {
		return stack.Spec.Version, nil
	}
	if stack.Spec.VersionsFromFile != "" {
		versions := &v1beta1.Versions{}
		err := ctx.GetClient().Get(ctx, types.NamespacedName{
			Name: stack.Spec.VersionsFromFile,
		}, versions)
		if client.IgnoreNotFound(err) != nil {
			return "", err
		}
		if err == nil {
			version, ok := versions.Spec[kind]
			if ok && version != "" {
				return version, nil
			}
		}
		return "", fmt.Errorf("%w for module %s on stack %s: module not found in Versions resource %s", ErrNoVersionFound, kind, stack.Name, stack.Spec.VersionsFromFile)
	}

	return "", fmt.Errorf("%w for module %s on stack %s: stack must define spec.version, spec.versionsFromFile, or the module must define its own version", ErrNoVersionFound, kind, stack.Name)
}

func IsGreaterOrEqual(version string, than string) bool {
	if !semver.IsValid(than) {
		return !semver.IsValid(version) // Any semver version is considered lower
	}
	if !semver.IsValid(version) {
		return true
	}
	return semver.Compare(version, than) >= 0
}

func IsLower(version string, than string) bool {
	if !semver.IsValid(than) {
		return semver.IsValid(version) // Any semver version is considered higher
	}
	if !semver.IsValid(version) {
		return false
	}
	return semver.Compare(version, than) < 0
}
