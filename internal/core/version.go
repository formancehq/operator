package core

import (
	"errors"
	"fmt"
	"regexp"
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

// partialSemverRe matches `v<major>` or `v<major>.<minor>` — semver-shaped
// names that are missing the patch (and optionally the minor) component.
// These are routinely used as Versions resource names (`v3`, `v3.2`) and
// would otherwise sail past [semver.IsValid] and silently bypass the
// minimum-version check.
var partialSemverRe = regexp.MustCompile(`^v(\d+)(?:\.(\d+))?$`)

// normalizePartialSemver expands `v3` → `v3.0.0` and `v3.2` → `v3.2.0` so
// partial-semver Versions resource names are gated by the same min-version
// check as their canonical form. Non-matching inputs (canonical semver,
// dev tags, SHA refs, non-`v`-prefixed strings) are returned unchanged.
func normalizePartialSemver(v string) string {
	m := partialSemverRe.FindStringSubmatch(v)
	if m == nil {
		return v
	}
	minor := m[2]
	if minor == "" {
		minor = "0"
	}
	return fmt.Sprintf("v%s.%s.0", m[1], minor)
}

// ValidateMinimumVersion checks that a Versions resource name meets the minimum requirement.
// Non-semver names (dev tags, SHA refs, non-`v`-prefixed strings) are allowed through.
func ValidateMinimumVersion(version string) error {
	normalized := normalizePartialSemver(version)
	if semver.IsValid(normalized) && semver.Compare(normalized, MinimumStackVersion) < 0 {
		return fmt.Errorf("version %s is not supported, minimum required: %s - please upgrade your stack", version, MinimumStackVersion)
	}
	return nil
}

func ResolveModuleVersion(ctx Context, stack *v1beta1.Stack, module v1beta1.Module) (string, error) {
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

	return version, nil
}

func GetModuleVersion(ctx Context, stack *v1beta1.Stack, module v1beta1.Module) (string, error) {
	if module.GetVersion() == "" && stack.Spec.Version == "" && stack.Spec.VersionsFromFile != "" {
		if err := ValidateMinimumVersion(stack.Spec.VersionsFromFile); err != nil {
			return "", err
		}
	}

	version, err := ResolveModuleVersion(ctx, stack, module)
	if err != nil {
		return "", err
	}

	return version, nil
}
