package stacks

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
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
	require.NoError(t, corev1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&v1beta1.ResourceReference{}).
		WithIndex(&v1beta1.Settings{}, "stack", func(o client.Object) []string { return o.(*v1beta1.Settings).GetStacks() }).
		WithIndex(&v1beta1.Settings{}, "keylen", func(o client.Object) []string {
			return []string{fmt.Sprint(len(settings.SplitKeywordWithDot(o.(*v1beta1.Settings).Spec.Key)))}
		}).Build()
	return policyTestContext{Context: context.Background(), c: c, s: scheme}
}
func TestPluginSourceSettingOverridesAndValidation(t *testing.T) {
	ctx := newPolicyTestContext(t, settings.New("global", settings.ConnectivityInstancePullSecrets, " source ", "*"), settings.New("empty", settings.ConnectivityInstancePullSecrets, "", "one"))
	value, err := settings.GetConnectivityInstancePullSecret(ctx, "one")
	require.NoError(t, err)
	require.Empty(t, value)
	value, err = settings.GetConnectivityInstancePullSecret(ctx, "two")
	require.NoError(t, err)
	require.Equal(t, "source", value)
	for _, value := range []string{"source,other", "bad/name"} {
		invalid := settings.New("invalid", settings.ConnectivityInstancePullSecrets, value, "two")
		require.NoError(t, ctx.c.Create(ctx, invalid))
		_, err := settings.GetConnectivityInstancePullSecret(ctx, "two")
		require.ErrorContains(t, err, "one valid source Secret name")
		require.NoError(t, ctx.c.Delete(ctx, invalid))
	}
}
func TestPluginSettingWatchKeys(t *testing.T) {
	for _, key := range []string{settings.ConnectivityInstancePullSecrets, "*.instances.image-pull-secrets", "*.*.*"} {
		require.True(t, settings.IsConnectivityPullSecretSetting(key), key)
	}
	for _, key := range []string{"connectivity.image-pull-secrets", "namespace.labels", "registries.*.endpoint"} {
		require.False(t, settings.IsConnectivityPullSecretSetting(key), key)
	}
}
func TestPluginReferenceStableTargetAndSourceGeneration(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant", UID: "stack"}}
	ctx := newPolicyTestContext(t, stack)
	require.Error(t, reconcilePullSecretReferences(ctx, stack, "source"))
	ref := &v1beta1.ResourceReference{}
	key := client.ObjectKey{Name: "tenant-ghcr-pull-connectivity-plugins"}
	require.NoError(t, ctx.c.Get(ctx, key, ref))
	require.Equal(t, "source", ref.Spec.Name)
	require.Equal(t, instancePullSecretTarget, ref.Spec.TargetName)
	require.True(t, metav1.IsControlledBy(ref, stack))
	ref.Status.Ready = true
	require.NoError(t, ctx.c.Status().Update(ctx, ref))
	require.NoError(t, reconcilePullSecretReferences(ctx, stack, "replacement"))
	require.NoError(t, ctx.c.Get(ctx, key, ref))
	require.Equal(t, "replacement", ref.Spec.Name)
	require.Equal(t, instancePullSecretTarget, ref.GetTargetName())
	ref.Generation = 2
	require.NoError(t, ctx.c.Update(ctx, ref))
	ref.Status.Ready = true
	ref.Status.ObservedGeneration = 1
	require.NoError(t, ctx.c.Status().Update(ctx, ref))
	require.Error(t, reconcilePullSecretReferences(ctx, stack, "replacement"))
	require.NoError(t, reconcilePullSecretReferences(ctx, stack, ""))
	require.Error(t, ctx.c.Get(ctx, key, ref))
}
func TestLocalPluginDestinationStillGetsReferenceAndReportsCollision(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant", UID: "stack"}}
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default", Labels: map[string]string{v1beta1.StackLabel: "tenant"}}, Data: map[string][]byte{"credential": []byte("source")}}
	local := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: instancePullSecretTarget, Namespace: stack.Name}, Data: map[string][]byte{"credential": []byte("local")}}
	ctx := newPolicyTestContext(t, stack, source, local)
	require.Error(t, reconcilePullSecretReferences(ctx, stack, source.Name))
	ref := &v1beta1.ResourceReference{}
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKey{Name: "tenant-" + instancePullSecretTarget}, ref))
	require.ErrorContains(t, resourcereferences.Reconcile(ctx, stack, ref), "refusing to overwrite unmanaged pull secret")
	got := &corev1.Secret{}
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(local), got))
	require.Equal(t, local.Data, got.Data)
	require.Empty(t, got.OwnerReferences)
}

func TestSharedPluginPolicyAggregatesStacksAndReconcilesDrift(t *testing.T) {
	var objects []client.Object
	for i := 0; i < 30; i++ {
		objects = append(objects, &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("tenant-%02d", i), UID: types.UID(fmt.Sprintf("stack-%d", i))}})
	}
	global := settings.New("global", settings.ConnectivityInstancePullSecrets, "source", "*")
	empty := settings.New("exclude", settings.ConnectivityInstancePullSecrets, "", "tenant-07")
	objects = append(objects, global, empty)
	ctx := newPolicyTestContext(t, objects...)
	// No source or destination Secrets exist: aggregation is independent of copies.
	require.NoError(t, reconcileSharedPluginPolicy(ctx))
	policy := pluginPolicyObject()
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy))
	require.Empty(t, policy.GetOwnerReferences())
	require.Equal(t, "true", policy.GetLabels()[sharedPluginPolicyMarker])
	spec := policy.Object["spec"].(map[string]any)
	require.NotContains(t, spec, "connectivitySelector")
	require.Empty(t, spec["instanceSelector"])
	expressions, found, err := unstructured.NestedSlice(policy.Object, "spec", "namespaceSelector", "matchExpressions")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, expressions, 1)
	expression := expressions[0].(map[string]any)
	require.Equal(t, "In", expression["operator"])
	require.Equal(t, "kubernetes.io/metadata.name", expression["key"])
	values := expression["values"].([]any)
	require.Len(t, values, 29)
	require.NotContains(t, values, "tenant-07")
	require.Equal(t, "tenant-00", values[0])
	require.Equal(t, "tenant-29", values[len(values)-1])
	list := &unstructured.UnstructuredList{}
	list.SetAPIVersion(policy.GetAPIVersion())
	list.SetKind("PolicyList")
	require.NoError(t, ctx.c.List(ctx, list))
	require.Len(t, list.Items, 1)
	rv := policy.GetResourceVersion()
	require.NoError(t, reconcileSharedPluginPolicy(ctx))
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy))
	require.Equal(t, rv, policy.GetResourceVersion())
	policy.Object["spec"].(map[string]any)["connectivitySelector"] = map[string]any{}
	require.NoError(t, ctx.c.Update(ctx, policy))
	require.NoError(t, reconcileSharedPluginPolicy(ctx))
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy))
	require.NotContains(t, policy.Object["spec"], "connectivitySelector")
	require.NoError(t, ctx.c.Delete(ctx, global))
	require.NoError(t, reconcileSharedPluginPolicy(ctx))
	require.True(t, apierrors.IsNotFound(ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy)))
}

func TestSharedPluginPolicyLastStackRemovalAndCollisionSafety(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant", UID: "stack"}}
	setting := settings.New("global", settings.ConnectivityInstancePullSecrets, "source", "*")
	ctx := newPolicyTestContext(t, stack, setting)
	require.NoError(t, reconcileSharedPluginPolicy(ctx))
	policy := pluginPolicyObject()
	require.NoError(t, ctx.c.Delete(ctx, stack))
	require.NoError(t, reconcileSharedPluginPolicy(ctx))
	require.True(t, apierrors.IsNotFound(ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy)))
	stack = &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant", UID: "replacement-stack"}}
	require.NoError(t, ctx.c.Create(ctx, stack))
	require.NoError(t, reconcileSharedPluginPolicy(ctx))
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy))
	require.NoError(t, ctx.c.Delete(ctx, policy))
	require.NoError(t, reconcileSharedPluginPolicy(ctx))
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy))
	for _, foreignOwner := range []bool{false, true} {
		if foreignOwner {
			policy.SetLabels(map[string]string{sharedPluginPolicyMarker: "true"})
			policy.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: "foreign", UID: "foreign"}})
		} else {
			policy.SetLabels(nil)
		}
		require.NoError(t, ctx.c.Update(ctx, policy))
		before := policy.DeepCopy()
		require.ErrorContains(t, reconcileSharedPluginPolicy(ctx), "refusing to overwrite")
		require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy))
		require.Equal(t, before, policy)
	}
	require.NoError(t, ctx.c.Delete(ctx, setting))
	require.ErrorContains(t, reconcileSharedPluginPolicy(ctx), "refusing to overwrite")
	require.NoError(t, ctx.c.Get(ctx, client.ObjectKeyFromObject(policy), policy))
}
func TestOptionalPluginPolicyAPIDoesNotRequireConnectivity(t *testing.T) {
	require.False(t, pluginPolicyAPIAvailable(newPolicyTestContext(t)))
}

func TestPluginCapabilityRequirementIsExplicitAndOptional(t *testing.T) {
	previous := pluginPolicyAvailable
	pluginPolicyAvailable = false
	t.Cleanup(func() { pluginPolicyAvailable = previous })
	require.NoError(t, requirePluginPolicyCapability(""))
	require.ErrorContains(t, requirePluginPolicyCapability("source"), "restart Stack Operator")
	pluginPolicyAvailable = true
	require.NoError(t, requirePluginPolicyCapability("source"))
}
