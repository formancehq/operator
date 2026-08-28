package core

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

const (
	DependenciesSatisfiedCondition = "DependenciesSatisfied"

	requirementsSatisfiedReason           = "RequirementsSatisfied"
	dependencyNotFoundReason              = "DependencyNotFound"
	multipleDependenciesFoundReason       = "MultipleDependenciesFound"
	dependencyNotReadyReason              = "DependencyNotReady"
	dependencyVersionMismatchReason       = "DependencyVersionMismatch"
	dependencyVersionUnresolvedReason     = "DependencyVersionUnresolved"
	dependencyVersionNotSemverReason      = "DependencyVersionNotSemVer"
	dependencyLookupFailedReason          = "DependencyLookupFailed"
	reconciledWithStackCondition          = "ReconciledWithStack"
	reconciledWithStackDependenciesReason = "Dependencies"
)

// ModuleRequirements is an explicit declaration of the objects that must be
// available before a module reconciler may run. Its zero value is invalid so a
// new module cannot omit the dependency decision accidentally.
type ModuleRequirements struct {
	declared     bool
	independent  bool
	requirements []Requirement
}

// Requirement describes one mandatory Stack-bound dependency.
//
// Fields are private so declarations can only be built with Require and its
// options, and validated once when the controller is set up.
type Requirement struct {
	dependency          v1beta1.Dependent
	requireReady        bool
	minVersionInclusive string
	maxVersionExclusive string
	minVersionSet       bool
	maxVersionSet       bool
	configurationErrors []string
}

// RequirementOption adds one constraint to a required dependency.
type RequirementOption interface {
	apply(*Requirement)
}

type requirementOptionFunc func(*Requirement)

func (option requirementOptionFunc) apply(requirement *Requirement) {
	option(requirement)
}

// Requirements declares one or more mandatory dependencies.
func Requirements(requirements ...Requirement) ModuleRequirements {
	return ModuleRequirements{
		declared:     true,
		requirements: requirements,
	}
}

// NoRequirements explicitly declares that a module has no mandatory
// dependencies.
func NoRequirements() ModuleRequirements {
	return ModuleRequirements{
		declared:    true,
		independent: true,
	}
}

// Require declares a Stack-bound object as a mandatory dependency.
func Require(dependency v1beta1.Dependent, options ...RequirementOption) Requirement {
	requirement := Requirement{dependency: dependency}
	for _, option := range options {
		option.apply(&requirement)
	}
	return requirement
}

// Ready requires the dependency to report status.ready=true.
func Ready() RequirementOption {
	return requirementOptionFunc(func(requirement *Requirement) {
		requirement.requireReady = true
	})
}

// VersionAtLeast sets an inclusive lower bound on the dependency's effective
// module version.
func VersionAtLeast(minInclusive string) RequirementOption {
	return requirementOptionFunc(func(requirement *Requirement) {
		requirement.setMinVersion(minInclusive)
	})
}

// VersionBefore sets an exclusive upper bound on the dependency's effective
// module version.
func VersionBefore(maxExclusive string) RequirementOption {
	return requirementOptionFunc(func(requirement *Requirement) {
		requirement.setMaxVersion(maxExclusive)
	})
}

// VersionBetween sets an inclusive lower bound and an exclusive upper bound on
// the dependency's effective module version.
func VersionBetween(minInclusive, maxExclusive string) RequirementOption {
	return requirementOptionFunc(func(requirement *Requirement) {
		requirement.setMinVersion(minInclusive)
		requirement.setMaxVersion(maxExclusive)
	})
}

func (requirement *Requirement) setMinVersion(version string) {
	if requirement.minVersionSet {
		requirement.configurationErrors = append(requirement.configurationErrors, "minimum version is declared more than once")
		return
	}
	requirement.minVersionSet = true
	requirement.minVersionInclusive = version
}

func (requirement *Requirement) setMaxVersion(version string) {
	if requirement.maxVersionSet {
		requirement.configurationErrors = append(requirement.configurationErrors, "maximum version is declared more than once")
		return
	}
	requirement.maxVersionSet = true
	requirement.maxVersionExclusive = version
}

func (requirements ModuleRequirements) hasRequirements() bool {
	return len(requirements.requirements) > 0
}

func (requirements ModuleRequirements) dependencies() []v1beta1.Dependent {
	dependencies := make([]v1beta1.Dependent, 0, len(requirements.requirements))
	for _, requirement := range requirements.requirements {
		dependencies = append(dependencies, requirement.dependency)
	}
	return dependencies
}

func (requirements ModuleRequirements) validate(scheme *runtime.Scheme) error {
	switch {
	case !requirements.declared:
		return errors.New("module requirements must be declared with Requirements or NoRequirements")
	case requirements.independent && len(requirements.requirements) != 0:
		return errors.New("NoRequirements cannot contain dependencies")
	case !requirements.independent && len(requirements.requirements) == 0:
		return errors.New("Requirements must contain at least one dependency; use NoRequirements otherwise")
	}

	seen := map[string]struct{}{}
	for index, requirement := range requirements.requirements {
		if isNilDependent(requirement.dependency) {
			return fmt.Errorf("requirement %d has a nil dependency", index)
		}
		if reflect.TypeOf(requirement.dependency).Kind() != reflect.Pointer {
			return fmt.Errorf("requirement %d dependency must be a pointer", index)
		}
		if len(requirement.configurationErrors) > 0 {
			return fmt.Errorf("requirement %d is invalid: %s", index, strings.Join(requirement.configurationErrors, ", "))
		}

		gvks, _, err := scheme.ObjectKinds(requirement.dependency)
		if err != nil {
			return fmt.Errorf("resolving requirement %d dependency kind: %w", index, err)
		}
		if len(gvks) == 0 {
			return fmt.Errorf("requirement %d dependency has no registered kind", index)
		}
		key := gvks[0].GroupKind().String()
		if _, ok := seen[key]; ok {
			return fmt.Errorf("dependency %s is required more than once", key)
		}
		seen[key] = struct{}{}

		if requirement.hasVersionConstraint() {
			if _, ok := requirement.dependency.(v1beta1.Module); !ok {
				return fmt.Errorf("dependency %s does not implement Module and cannot have a version constraint", key)
			}
			if requirement.minVersionSet && requirement.minVersionInclusive == "" {
				return fmt.Errorf("dependency %s minimum version cannot be empty", key)
			}
			if requirement.maxVersionSet && requirement.maxVersionExclusive == "" {
				return fmt.Errorf("dependency %s maximum version cannot be empty", key)
			}

			minVersion, err := normalizeRequiredSemver(requirement.minVersionInclusive)
			if err != nil {
				return fmt.Errorf("dependency %s minimum version: %w", key, err)
			}
			maxVersion, err := normalizeRequiredSemver(requirement.maxVersionExclusive)
			if err != nil {
				return fmt.Errorf("dependency %s maximum version: %w", key, err)
			}
			if minVersion != "" && maxVersion != "" && semver.Compare(minVersion, maxVersion) >= 0 {
				return fmt.Errorf("dependency %s version range must have a lower minimum than maximum", key)
			}
		}
	}

	return nil
}

func isNilDependent(dependency v1beta1.Dependent) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func (requirement Requirement) hasVersionConstraint() bool {
	return requirement.minVersionSet || requirement.maxVersionSet
}

func normalizeRequiredSemver(version string) (string, error) {
	if version == "" {
		return "", nil
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !semver.IsValid(version) {
		return "", fmt.Errorf("%q is not a valid semantic version", version)
	}
	return version, nil
}

type requirementsEvaluation struct {
	status  metav1.ConditionStatus
	reason  string
	message string
	cause   error
}

type requirementFinding struct {
	kind       string
	sortKey    string
	evaluation requirementsEvaluation
}

func evaluateModuleRequirements(ctx Context, stack *v1beta1.Stack, requirements ModuleRequirements) requirementsEvaluation {
	if !requirements.hasRequirements() {
		return requirementsEvaluation{
			status:  metav1.ConditionTrue,
			reason:  requirementsSatisfiedReason,
			message: "Module declares no dependencies",
		}
	}

	findings := make([]requirementFinding, 0, len(requirements.requirements))
	for _, requirement := range requirements.requirements {
		kind, sortKey := requirementIdentity(ctx, requirement)
		if evaluation := evaluateRequirement(ctx, stack, requirement, kind); evaluation.status != metav1.ConditionTrue {
			findings = append(findings, requirementFinding{kind: kind, sortKey: sortKey, evaluation: evaluation})
		}
	}

	if len(findings) == 0 {
		return requirementsEvaluation{
			status:  metav1.ConditionTrue,
			reason:  requirementsSatisfiedReason,
			message: "All module dependencies are satisfied",
		}
	}

	slices.SortFunc(findings, func(a, b requirementFinding) int {
		return strings.Compare(a.sortKey, b.sortKey)
	})
	selected := findings[0].evaluation
	for _, finding := range findings {
		if finding.evaluation.status == metav1.ConditionFalse {
			selected = finding.evaluation
			break
		}
	}

	messages := make([]string, 0, len(findings))
	for _, finding := range findings {
		messages = append(messages, finding.evaluation.message)
	}
	selected.message = strings.Join(messages, "; ")
	if selected.status == metav1.ConditionUnknown {
		for _, finding := range findings {
			if finding.evaluation.cause != nil {
				selected.cause = finding.evaluation.cause
				break
			}
		}
	}
	return selected
}

func evaluateRequirement(ctx Context, stack *v1beta1.Stack, requirement Requirement, kind string) requirementsEvaluation {
	dependency := requirement.dependency.DeepCopyObject().(v1beta1.Dependent)
	err := GetSingleDependency(ctx, stack.Name, dependency)
	switch {
	case errors.Is(err, ErrNotFound):
		return requirementsEvaluation{
			status:  metav1.ConditionFalse,
			reason:  dependencyNotFoundReason,
			message: fmt.Sprintf("required dependency %s was not found in Stack %s", kind, stack.Name),
		}
	case errors.Is(err, ErrMultipleInstancesFound):
		return requirementsEvaluation{
			status:  metav1.ConditionFalse,
			reason:  multipleDependenciesFoundReason,
			message: fmt.Sprintf("multiple dependencies of kind %s were found in Stack %s", kind, stack.Name),
		}
	case err != nil:
		return requirementsEvaluation{
			status:  metav1.ConditionUnknown,
			reason:  dependencyLookupFailedReason,
			message: fmt.Sprintf("could not resolve dependency %s in Stack %s: %s", kind, stack.Name, err),
			cause:   err,
		}
	}

	if requirement.hasVersionConstraint() {
		module := dependency.(v1beta1.Module)
		version, err := ResolveModuleVersion(ctx, stack, module)
		if err != nil {
			evaluation := requirementsEvaluation{
				status:  metav1.ConditionUnknown,
				reason:  dependencyVersionUnresolvedReason,
				message: fmt.Sprintf("could not resolve effective version for dependency %s: %s", kind, err),
			}
			if !errors.Is(err, ErrNoVersionFound) {
				evaluation.cause = err
			}
			return evaluation
		}
		normalizedVersion, err := normalizeRequiredSemver(version)
		if err != nil {
			return requirementsEvaluation{
				status:  metav1.ConditionUnknown,
				reason:  dependencyVersionNotSemverReason,
				message: fmt.Sprintf("dependency %s has an unordered effective version %q", kind, version),
			}
		}

		minVersion, _ := normalizeRequiredSemver(requirement.minVersionInclusive)
		if minVersion != "" && semver.Compare(normalizedVersion, minVersion) < 0 {
			return requirementsEvaluation{
				status:  metav1.ConditionFalse,
				reason:  dependencyVersionMismatchReason,
				message: fmt.Sprintf("dependency %s effective version %s must be at least %s", kind, normalizedVersion, minVersion),
			}
		}
		maxVersion, _ := normalizeRequiredSemver(requirement.maxVersionExclusive)
		if maxVersion != "" && semver.Compare(normalizedVersion, maxVersion) >= 0 {
			return requirementsEvaluation{
				status:  metav1.ConditionFalse,
				reason:  dependencyVersionMismatchReason,
				message: fmt.Sprintf("dependency %s effective version %s must be before %s", kind, normalizedVersion, maxVersion),
			}
		}
	}

	if requirement.requireReady && !dependency.IsReady() {
		return requirementsEvaluation{
			status:  metav1.ConditionFalse,
			reason:  dependencyNotReadyReason,
			message: fmt.Sprintf("required dependency %s is not ready", kind),
		}
	}

	return requirementsEvaluation{
		status:  metav1.ConditionTrue,
		reason:  requirementsSatisfiedReason,
		message: fmt.Sprintf("dependency %s is satisfied", kind),
	}
}

func requirementIdentity(ctx Context, requirement Requirement) (kind string, sortKey string) {
	gvks, _, err := ctx.GetScheme().ObjectKinds(requirement.dependency)
	if err != nil || len(gvks) == 0 {
		fallback := reflect.TypeOf(requirement.dependency).String()
		return fallback, fallback
	}
	return gvks[0].Kind, gvks[0].GroupKind().String()
}

func setModuleRequirementsCondition(module v1beta1.Module, evaluation requirementsEvaluation) {
	transitionTime := metav1.Now()
	if existing := module.GetConditions().Get(DependenciesSatisfiedCondition); existing != nil &&
		existing.Status == evaluation.status && existing.Reason == evaluation.reason && existing.Message == evaluation.message {
		transitionTime = existing.LastTransitionTime
	}
	module.GetConditions().AppendOrReplace(v1beta1.Condition{
		Type:               DependenciesSatisfiedCondition,
		Status:             evaluation.status,
		ObservedGeneration: module.GetGeneration(),
		LastTransitionTime: transitionTime,
		Reason:             evaluation.reason,
		Message:            evaluation.message,
	}, v1beta1.ConditionTypeMatch(DependenciesSatisfiedCondition))
}

func setRequirementsReconciledWithStackCondition(module v1beta1.Module, stack *v1beta1.Stack, evaluation requirementsEvaluation) {
	module.GetConditions().AppendOrReplace(v1beta1.Condition{
		Type:               reconciledWithStackCondition,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: stack.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             reconciledWithStackDependenciesReason,
		Message:            evaluation.message,
	}, v1beta1.ConditionTypeMatch(reconciledWithStackCondition))
}
