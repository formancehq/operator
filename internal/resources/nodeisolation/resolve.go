package nodeisolation

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

const (
	// ModeOrganization isolates all stacks of an organization on a shared pool named
	// after the organization. ModeStack gives every stack a dedicated pool named
	// "<organization>-<stack>".
	ModeOrganization = "organization"
	ModeStack        = "stack"

	// ManagedByLabel/ManagedByValue are stamped on every Karpenter object created by the
	// operator so the cleanup sweep can select them without relying on owner references
	// (which the module cleanup path cannot see for cluster-scoped objects).
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "formance-operator"

	EC2NodeClassKind = "EC2NodeClass"
	NodePoolKind     = "NodePool"

	defaultEC2NodeClassGroupVersion = "karpenter.k8s.aws/v1"
	defaultNodePoolGroupVersion     = "karpenter.sh/v1"
)

// ManagedSchedulingKeys are the nodeSelector/toleration keys the operator injects into
// workloads. They are the pool-identity dimensions (organization, and stack in
// dedicated-stack mode). Customer is deliberately NOT a scheduling key: in a shared
// organization pool, stacks may resolve different customer values, so tainting/tolerating
// on customer would break scheduling. Customer remains an EC2 tag / node label for cost
// attribution only. Consumers strip these keys before re-applying, so placement stays
// idempotent across mode changes, relabels, and disable.
var ManagedSchedulingKeys = []string{v1beta1.OrganizationLabel, v1beta1.StackLabel}

// Config is the resolved node-isolation intent for a single stack. It is the single
// source of truth shared by the Karpenter reconciler (which materializes the pools) and
// the applications mutator (which pins workloads onto them).
type Config struct {
	Enabled  bool
	Mode     string
	PoolName string

	Organization string
	Customer     string
	Stack        string

	// Tags is the map injected into EC2NodeClass spec.tags and NodePool node labels.
	Tags         map[string]string
	Taints       []corev1.Taint
	Tolerations  []corev1.Toleration
	NodeSelector map[string]string

	EC2NodeClassGVK schema.GroupVersionKind
	NodePoolGVK     schema.GroupVersionKind
	EC2NodeClassRef string
	NodePoolRef     string
}

// Resolve reads the karpenter.* settings and the Stack identity labels and computes the
// desired isolation Config. The Karpenter GVKs are always populated (even when disabled)
// so the cleanup sweep can locate previously created objects. When disabled, only
// Enabled/GVKs are meaningful.
func Resolve(ctx core.Context, stack *v1beta1.Stack) (*Config, error) {
	ec2GVK, err := resolveGVK(ctx, stack.Name, EC2NodeClassKind, defaultEC2NodeClassGroupVersion, "ec2-node-class")
	if err != nil {
		return nil, err
	}
	nodePoolGVK, err := resolveGVK(ctx, stack.Name, NodePoolKind, defaultNodePoolGroupVersion, "node-pool")
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		EC2NodeClassGVK: ec2GVK,
		NodePoolGVK:     nodePoolGVK,
	}

	enabled, err := settings.GetBoolOrFalse(ctx, stack.Name, "karpenter", "enabled")
	if err != nil {
		return nil, err
	}
	cfg.Enabled = enabled
	if !enabled {
		return cfg, nil
	}

	mode, err := settings.GetStringOrDefault(ctx, stack.Name, ModeOrganization, "karpenter", "isolation")
	if err != nil {
		return nil, err
	}
	if mode != ModeOrganization && mode != ModeStack {
		return nil, core.NewPendingError().WithMessage(
			"invalid karpenter.isolation %q, expected %q or %q", mode, ModeOrganization, ModeStack)
	}
	cfg.Mode = mode

	labels := stack.GetLabels()
	org := labels[v1beta1.OrganizationLabel]
	if org == "" {
		return nil, core.NewPendingError().WithMessage(
			"stack label %s is required for karpenter node isolation", v1beta1.OrganizationLabel)
	}
	customer := labels[v1beta1.CustomerLabel]
	if customer == "" {
		customer = org
	}
	cfg.Organization = org
	cfg.Customer = customer
	cfg.Stack = stack.Name

	switch mode {
	case ModeOrganization:
		cfg.PoolName = org
	case ModeStack:
		cfg.PoolName = fmt.Sprintf("%s-%s", org, stack.Name)
	}
	if errs := validation.IsDNS1123Subdomain(cfg.PoolName); len(errs) > 0 {
		return nil, core.NewPendingError().WithMessage(
			"computed karpenter pool name %q is invalid: %s", cfg.PoolName, strings.Join(errs, ", "))
	}

	// Tags/node-labels: customer + organization always; stack only in dedicated-stack mode
	// (an organization pool is shared, so a single stack value would be misleading).
	tags := map[string]string{
		v1beta1.CustomerLabel:     customer,
		v1beta1.OrganizationLabel: org,
	}
	if mode == ModeStack {
		tags[v1beta1.StackLabel] = stack.Name
	}
	for k, v := range tags {
		if errs := validation.IsValidLabelValue(v); len(errs) > 0 {
			return nil, core.NewPendingError().WithMessage(
				"label value for %s (%q) is invalid: %s", k, v, strings.Join(errs, ", "))
		}
	}
	cfg.Tags = tags

	// Scheduling identity = the pool-identity dimensions only (organization, plus stack in
	// dedicated-stack mode). NodeSelector enforces placement; taints/tolerations (keyed on
	// the same identity) repel non-dedicated workloads. Customer is intentionally excluded
	// from scheduling (see ManagedSchedulingKeys).
	cfg.NodeSelector = map[string]string{v1beta1.OrganizationLabel: org}
	if mode == ModeStack {
		cfg.NodeSelector[v1beta1.StackLabel] = stack.Name
	}
	for _, key := range slices.Sorted(maps.Keys(cfg.NodeSelector)) {
		cfg.Taints = append(cfg.Taints, corev1.Taint{
			Key:    key,
			Value:  cfg.NodeSelector[key],
			Effect: corev1.TaintEffectNoSchedule,
		})
		cfg.Tolerations = append(cfg.Tolerations, corev1.Toleration{
			Key:      key,
			Operator: corev1.TolerationOpEqual,
			Value:    cfg.NodeSelector[key],
			Effect:   corev1.TaintEffectNoSchedule,
		})
	}

	cfg.EC2NodeClassRef, err = settings.GetStringOrEmpty(ctx, stack.Name, "karpenter", "ec2-node-class", "reference")
	if err != nil {
		return nil, err
	}
	cfg.NodePoolRef, err = settings.GetStringOrEmpty(ctx, stack.Name, "karpenter", "node-pool", "reference")
	if err != nil {
		return nil, err
	}
	if cfg.EC2NodeClassRef == "" || cfg.NodePoolRef == "" {
		return nil, core.NewPendingError().WithMessage(
			"karpenter.ec2-node-class.reference and karpenter.node-pool.reference settings are required")
	}

	return cfg, nil
}

func resolveGVK(ctx core.Context, stack, kind, defaultGroupVersion, settingSegment string) (schema.GroupVersionKind, error) {
	gv, err := settings.GetStringOrDefault(ctx, stack, defaultGroupVersion, "karpenter", "api", settingSegment, "group-version")
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	parsed, err := schema.ParseGroupVersion(gv)
	if err != nil {
		return schema.GroupVersionKind{}, core.NewPendingError().WithMessage(
			"invalid group-version %q for %s: %s", gv, kind, err)
	}
	return parsed.WithKind(kind), nil
}
