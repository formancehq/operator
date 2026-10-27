package registries

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

type registryContext struct {
	context.Context
	client client.Client
	scheme *runtime.Scheme
}

func (c *registryContext) GetClient() client.Client    { return c.client }
func (c *registryContext) GetAPIReader() client.Reader { return c.client }
func (c *registryContext) GetScheme() *runtime.Scheme  { return c.scheme }
func (c *registryContext) GetPlatform() core.Platform  { return core.Platform{} }
func testContext(t *testing.T, objects ...client.Object) core.Context {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithStatusSubresource(&v1beta1.ResourceReference{}).
		WithIndex(&v1beta1.Settings{}, "stack", func(o client.Object) []string { return o.(*v1beta1.Settings).GetStacks() }).
		WithIndex(&v1beta1.Settings{}, "keylen", func(o client.Object) []string {
			return []string{fmt.Sprint(len(settings.SplitKeywordWithDot(o.(*v1beta1.Settings).Spec.Key)))}
		}).Build()
	return &registryContext{Context: context.Background(), client: c, scheme: scheme}
}

func TestPullSecretNames(t *testing.T) {
	for _, tc := range []struct {
		name    string
		objects []client.Object
		want    []string
		invalid bool
	}{
		{name: "quoted registry", objects: []client.Object{settings.New("ghcr", `registries."ghcr.io".endpoint`, "ghcr.io?pullSecret=ghcr", "*")}, want: []string{"ghcr"}},
		{name: "stack override removes credentials", objects: []client.Object{settings.New("global", `registries."ghcr.io".endpoint`, "ghcr.io?pullSecret=ghcr", "*"), settings.New("local", `registries."ghcr.io".endpoint`, "public.example", "stack")}, want: []string{}},
		{name: "wildcard and deduplication", objects: []client.Object{settings.New("global", "registries.*.endpoint", "mirror?pullSecret=shared,pullSecret=shared", "*"), settings.New("other", `registries."ghcr.io".endpoint`, "ghcr.io?pullSecret=private", "stack"), settings.New("unrelated", `registries."other.io".endpoint`, "bad?broken", "another-stack")}, want: []string{"private", "shared"}},
		{name: "invalid", objects: []client.Object{settings.New("bad", "registries.*.endpoint", "mirror?broken", "*")}, invalid: true},
		{name: "empty name", objects: []client.Object{settings.New("bad", "registries.*.endpoint", "mirror?pullSecret=", "*")}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names, err := pullSecretNames(testContext(t, tc.objects...), "stack")
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, names)
			}
		})
	}
}

func TestPullSecretReferenceLifecycle(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack", UID: "stack-uid"}}
	setting := settings.New("registry", `registries."ghcr.io".endpoint`, "ghcr.io?pullSecret=first,pullSecret=first", "*")
	ctx := testContext(t, stack, setting)
	require.Error(t, ReconcilePullSecretReferences(ctx, stack))
	refs := &v1beta1.ResourceReferenceList{}
	require.NoError(t, ctx.GetClient().List(ctx, refs))
	require.Len(t, refs.Items, 1)
	first := refs.Items[0].DeepCopy()
	require.True(t, metav1.IsControlledBy(first, stack))
	require.Equal(t, "first", first.Spec.Name)
	first.Status.Ready = true
	require.NoError(t, ctx.GetClient().Status().Update(ctx, first))
	require.NoError(t, ReconcilePullSecretReferences(ctx, stack))
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: setting.Name}, setting))
	setting.Spec.Value = "ghcr.io?pullSecret=second"
	require.NoError(t, ctx.GetClient().Update(ctx, setting))
	require.Error(t, ReconcilePullSecretReferences(ctx, stack))
	require.NoError(t, ctx.GetClient().List(ctx, refs))
	require.Len(t, refs.Items, 2, "retain old reference until replacement ready")
	for _, ref := range refs.Items {
		if ref.Spec.Name == "second" {
			ref.Status.Ready = true
			require.NoError(t, ctx.GetClient().Status().Update(ctx, &ref))
		}
	}
	require.NoError(t, ReconcilePullSecretReferences(ctx, stack))
	require.NoError(t, ctx.GetClient().List(ctx, refs))
	require.Len(t, refs.Items, 1)
	require.Equal(t, "second", refs.Items[0].Spec.Name)
	require.NoError(t, ctx.GetClient().Delete(ctx, setting))
	require.NoError(t, ReconcilePullSecretReferences(ctx, stack))
	require.NoError(t, ctx.GetClient().List(ctx, refs))
	require.Empty(t, refs.Items)
}

func TestLocalPullSecretIsPreserved(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack", UID: "uid"}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: stack.Name}, Data: map[string][]byte{"test": []byte("local-test-value")}}
	ctx := testContext(t, stack, secret, settings.New("registry", "registries.*.endpoint", "mirror?pullSecret=local", "*"))
	require.NoError(t, ReconcilePullSecretReferences(ctx, stack))
	refs := &v1beta1.ResourceReferenceList{}
	require.NoError(t, ctx.GetClient().List(ctx, refs))
	require.Empty(t, refs.Items)
	actual := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(secret), actual))
	require.Equal(t, secret.Data, actual.Data)
	require.Empty(t, actual.OwnerReferences)
}

func TestAdditionalPullSecretReferencesWithoutRegistrySettings(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack", UID: "uid"}}
	ctx := testContext(t, stack)
	require.Error(t, ReconcilePullSecretReferences(ctx, stack, "core", "shared", "shared", "plugin"))
	refs := &v1beta1.ResourceReferenceList{}
	require.NoError(t, ctx.GetClient().List(ctx, refs))
	require.Len(t, refs.Items, 3)
	for i := range refs.Items {
		refs.Items[i].Status.Ready = true
		require.NoError(t, ctx.GetClient().Status().Update(ctx, &refs.Items[i]))
	}
	// Clearing only Core/API retains the shared Secret required by plugins.
	require.NoError(t, ReconcilePullSecretReferences(ctx, stack, "shared", "plugin"))
	require.NoError(t, ctx.GetClient().List(ctx, refs))
	require.Len(t, refs.Items, 2)
	for _, ref := range refs.Items {
		require.NotEqual(t, "core", ref.Spec.Name)
		require.True(t, metav1.IsControlledBy(&ref, stack))
	}
	require.NoError(t, ReconcilePullSecretReferences(ctx, stack))
	require.NoError(t, ctx.GetClient().List(ctx, refs))
	require.Empty(t, refs.Items)
}
