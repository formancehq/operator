package stacks

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

type policyTestContext struct {
	context.Context
	c client.Client
	s *runtime.Scheme
}

func (c policyTestContext) GetClient() client.Client    { return c.c }
func (c policyTestContext) GetAPIReader() client.Reader { return c.c }
func (c policyTestContext) GetScheme() *runtime.Scheme  { return c.s }
func (c policyTestContext) GetPlatform() core.Platform  { return core.Platform{} }
func newPolicyTestContext(t *testing.T, objects ...client.Object) policyTestContext {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithIndex(&v1beta1.Settings{}, "stack", func(o client.Object) []string { return o.(*v1beta1.Settings).GetStacks() }).
		WithIndex(&v1beta1.Settings{}, "keylen", func(o client.Object) []string {
			return []string{fmt.Sprint(len(settings.SplitKeywordWithDot(o.(*v1beta1.Settings).Spec.Key)))}
		}).Build()
	return policyTestContext{Context: context.Background(), c: c, s: scheme}
}
func enablePolicyTest(t *testing.T) {
	t.Helper()
	previous := pullSecretPoliciesAvailable
	pullSecretPoliciesAvailable = true
	t.Cleanup(func() { pullSecretPoliciesAvailable = previous })
}
func policyFor(stack *v1beta1.Stack, axis string) *unstructured.Unstructured {
	p := &unstructured.Unstructured{}
	p.SetGroupVersionKind(pullSecretPolicyGVK)
	p.SetName(pullSecretPolicyName(stack, axis))
	return p
}
func TestPullSecretPoliciesIndependentLifecycle(t *testing.T) {
	enablePolicyTest(t)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant-one", UID: "stack-one"}}
	ctx := newPolicyTestContext(t, stack)
	require.NoError(t, reconcilePullSecretPolicies(ctx, stack, []string{"core"}, []string{"plugin"}))
	for axis, name := range map[string]string{"connectivitySelector": "core", "instanceSelector": "plugin"} {
		p := policyFor(stack, axis)
		require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(p), p))
		require.True(t, metav1.IsControlledBy(p, stack))
		spec := p.Object["spec"].(map[string]any)
		require.Contains(t, spec, axis)
		other := "instanceSelector"
		if axis == other {
			other = "connectivitySelector"
		}
		require.NotContains(t, spec, other)
		require.Equal(t, map[string]any{"matchLabels": map[string]any{"kubernetes.io/metadata.name": "tenant-one"}}, spec["namespaceSelector"])
		require.Equal(t, map[string]any{"imagePullSecrets": []any{map[string]any{"name": name}}}, spec["mutate"])
	}
	// An unchanged replay must not update the policy (including API defaults).
	before := policyFor(stack, "connectivitySelector")
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(before), before))
	require.NoError(t, reconcilePullSecretPolicies(ctx, stack, []string{"core"}, []string{"plugin"}))
	after := policyFor(stack, "connectivitySelector")
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(after), after))
	require.Equal(t, before.GetResourceVersion(), after.GetResourceVersion())
	// Removing one axis deletes its policy instead of relying on an empty list
	// to clear a last-write-wins mutation; the other axis remains independent.
	require.NoError(t, reconcilePullSecretPolicies(ctx, stack, nil, []string{"replacement"}))
	p := policyFor(stack, "connectivitySelector")
	require.Error(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(p), p))
	p = policyFor(stack, "instanceSelector")
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(p), p))
	names, _, err := unstructured.NestedSlice(p.Object, "spec", "mutate", "imagePullSecrets")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"name": "replacement"}}, names)
	require.NoError(t, reconcilePullSecretPolicies(ctx, stack, nil, nil))
	require.Error(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(p), p))
}
func TestPullSecretPolicyCollision(t *testing.T) {
	enablePolicyTest(t)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant", UID: "stack"}}
	manual := policyFor(stack, "connectivitySelector")
	manual.Object["spec"] = map[string]any{"mode": "Warn"}
	ctx := newPolicyTestContext(t, stack, manual)
	require.ErrorContains(t, reconcilePullSecretPolicies(ctx, stack, []string{"secret"}, nil), "refusing to overwrite")
	require.NoError(t, reconcilePullSecretPolicies(ctx, stack, nil, nil))
	got := policyFor(stack, "connectivitySelector")
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(got), got))
	require.Equal(t, manual.Object["spec"], got.Object["spec"])
}
func TestPullSecretPolicyUnavailable(t *testing.T) {
	previous := pullSecretPoliciesAvailable
	pullSecretPoliciesAvailable = false
	t.Cleanup(func() { pullSecretPoliciesAvailable = previous })
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "ordinary-stack", UID: "stack"}}
	ctx := newPolicyTestContext(t, stack)
	require.NoError(t, reconcilePullSecretPolicies(ctx, stack, nil, nil))
	require.ErrorContains(t, reconcilePullSecretPolicies(ctx, stack, nil, []string{"plugin"}), "Policies unavailable")
}
func TestConnectivityPullSecretSettingsIndependentOverrides(t *testing.T) {
	ctx := newPolicyTestContext(t,
		settings.New("core", settings.ConnectivityPullSecrets, " core,shared, core ", "*"),
		settings.New("plugins", settings.ConnectivityInstancePullSecrets, "plugin,shared", "*"),
		settings.New("empty-core", settings.ConnectivityPullSecrets, "", "one"))
	coreNames, pluginNames, err := settings.GetConnectivityPullSecrets(ctx, "one")
	require.NoError(t, err)
	require.Empty(t, coreNames)
	require.Equal(t, []string{"plugin", "shared"}, pluginNames)
	coreNames, pluginNames, err = settings.GetConnectivityPullSecrets(ctx, "two")
	require.NoError(t, err)
	require.Equal(t, []string{"core", "shared"}, coreNames)
	require.Equal(t, []string{"plugin", "shared"}, pluginNames)
	bad := settings.New("invalid", settings.ConnectivityInstancePullSecrets, "bad/name", "one")
	require.NoError(t, ctx.c.Create(ctx, bad))
	_, _, err = settings.GetConnectivityPullSecrets(ctx, "one")
	require.ErrorContains(t, err, "invalid Secret name")
}

func TestPullSecretPolicyDiscoveryAbsentDoesNotRequireWatch(t *testing.T) {
	previous := pullSecretPoliciesAvailable
	t.Cleanup(func() { pullSecretPoliciesAvailable = previous })
	ctx := newPolicyTestContext(t)
	require.NoError(t, apiextensionsv1.AddToScheme(ctx.s))
	options := core.ReconcilerOptions[*v1beta1.Stack]{}
	withPullSecretPolicyWatch()(&options)
	require.Len(t, options.Raws, 1)
	// No Policy CRD: discovery must return before registering a watch.
	require.NoError(t, options.Raws[0](ctx, nil))
	require.False(t, pullSecretPoliciesAvailable)
}

func TestConnectivityPullSecretSettingWatchKeys(t *testing.T) {
	for _, key := range []string{settings.ConnectivityPullSecrets, settings.ConnectivityInstancePullSecrets, "connectivity.*", "*.instances.image-pull-secrets", "*.*.*"} {
		require.True(t, settings.IsConnectivityPullSecretSetting(key), key)
	}
	for _, key := range []string{"connectivity.monitoring", "connectivity.instances", "registries.ghcr.endpoint", "namespace.labels"} {
		require.False(t, settings.IsConnectivityPullSecretSetting(key), key)
	}
}
