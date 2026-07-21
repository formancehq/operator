package nodeisolation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func newTestContext(t *testing.T, objects ...client.Object) *mockContext {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
			return obj.(*v1beta1.Settings).GetStacks()
		}).
		WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
			return []string{fmt.Sprint(len(settings.SplitKeywordWithDot(obj.(*v1beta1.Settings).Spec.Key)))}
		}).
		WithObjects(objects...).
		Build()

	return &mockContext{Context: context.Background(), client: fakeClient, scheme: scheme}
}

func setting(name, key, value string, stacks ...string) *v1beta1.Settings {
	return &v1beta1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1beta1.SettingsSpec{Stacks: stacks, Key: key, Value: value},
	}
}

func stackWithLabels(name string, labels map[string]string) *v1beta1.Stack {
	return &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func TestResolveDisabled(t *testing.T) {
	t.Parallel()
	ctx := newTestContext(t)
	cfg, err := Resolve(ctx, stackWithLabels("stack0", nil))
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	// GVKs are always populated so cleanup can find previously created objects.
	require.Equal(t, "karpenter.k8s.aws", cfg.EC2NodeClassGVK.Group)
	require.Equal(t, "karpenter.sh", cfg.NodePoolGVK.Group)
}

func TestResolveOrganizationMode(t *testing.T) {
	t.Parallel()
	ctx := newTestContext(t,
		setting("enabled", "karpenter.enabled", "true", "*"),
		setting("ec2", "karpenter.ec2-node-class.reference", "default", "*"),
		setting("np", "karpenter.node-pool.reference", "default", "*"),
	)
	stack := stackWithLabels("stack0", map[string]string{
		v1beta1.OrganizationLabel: "acme",
		v1beta1.CustomerLabel:     "acme-corp",
	})

	cfg, err := Resolve(ctx, stack)
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, ModeOrganization, cfg.Mode)
	require.Equal(t, "acme", cfg.PoolName)

	// Organization pool is shared: no stack tag/label.
	require.Equal(t, map[string]string{
		v1beta1.CustomerLabel:     "acme-corp",
		v1beta1.OrganizationLabel: "acme",
	}, cfg.Tags)
	require.NotContains(t, cfg.Tags, v1beta1.StackLabel)

	// NodeSelector enforces org placement only.
	require.Equal(t, map[string]string{v1beta1.OrganizationLabel: "acme"}, cfg.NodeSelector)

	// Scheduling keys on organization only (customer is not a scheduling key).
	require.Len(t, cfg.Taints, 1)
	require.Len(t, cfg.Tolerations, 1)
	require.Equal(t, v1beta1.OrganizationLabel, cfg.Taints[0].Key)
	for _, tol := range cfg.Tolerations {
		require.Equal(t, corev1.TaintEffectNoSchedule, tol.Effect)
		require.Equal(t, corev1.TolerationOpEqual, tol.Operator)
	}
}

func TestResolveStackMode(t *testing.T) {
	t.Parallel()
	ctx := newTestContext(t,
		setting("enabled", "karpenter.enabled", "true", "stack0"),
		setting("isolation", "karpenter.isolation", ModeStack, "stack0"),
		setting("ec2", "karpenter.ec2-node-class.reference", "default", "stack0"),
		setting("np", "karpenter.node-pool.reference", "default", "stack0"),
	)
	stack := stackWithLabels("stack0", map[string]string{v1beta1.OrganizationLabel: "acme"})

	cfg, err := Resolve(ctx, stack)
	require.NoError(t, err)
	require.Equal(t, ModeStack, cfg.Mode)
	require.Equal(t, "acme-stack0", cfg.PoolName)

	// Customer defaults to organization when the label is absent.
	require.Equal(t, "acme", cfg.Customer)
	require.Equal(t, map[string]string{
		v1beta1.CustomerLabel:     "acme",
		v1beta1.OrganizationLabel: "acme",
		v1beta1.StackLabel:        "stack0",
	}, cfg.Tags)
	require.Equal(t, map[string]string{
		v1beta1.OrganizationLabel: "acme",
		v1beta1.StackLabel:        "stack0",
	}, cfg.NodeSelector)
	// Scheduling keys on organization + stack (customer excluded).
	require.Len(t, cfg.Taints, 2)
}

func TestResolveMissingOrganizationIsPending(t *testing.T) {
	t.Parallel()
	ctx := newTestContext(t, setting("enabled", "karpenter.enabled", "true", "*"))
	_, err := Resolve(ctx, stackWithLabels("stack0", nil))
	require.Error(t, err)
}

func TestResolveInvalidModeIsPending(t *testing.T) {
	t.Parallel()
	ctx := newTestContext(t,
		setting("enabled", "karpenter.enabled", "true", "*"),
		setting("isolation", "karpenter.isolation", "bogus", "*"),
	)
	_, err := Resolve(ctx, stackWithLabels("stack0", map[string]string{v1beta1.OrganizationLabel: "acme"}))
	require.Error(t, err)
}

func TestResolveMissingReferencesIsPending(t *testing.T) {
	t.Parallel()
	ctx := newTestContext(t, setting("enabled", "karpenter.enabled", "true", "*"))
	_, err := Resolve(ctx, stackWithLabels("stack0", map[string]string{v1beta1.OrganizationLabel: "acme"}))
	require.Error(t, err)
}
