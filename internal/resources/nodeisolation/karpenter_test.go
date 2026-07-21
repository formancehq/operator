package nodeisolation

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

var (
	testEC2GVK      = schema.GroupVersionKind{Group: "karpenter.k8s.aws", Version: "v1", Kind: "EC2NodeClass"}
	testNodePoolGVK = schema.GroupVersionKind{Group: "karpenter.sh", Version: "v1", Kind: "NodePool"}
)

func registerKarpenter(scheme *runtime.Scheme, gvk schema.GroupVersionKind) {
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	listGVK := gvk
	listGVK.Kind += "List"
	scheme.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
}

func reference(gvk schema.GroupVersionKind, name string, spec map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)
	obj.Object["spec"] = spec
	return obj
}

func newKarpenterContext(t *testing.T, objects ...client.Object) *mockContext {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	registerKarpenter(scheme, testEC2GVK)
	registerKarpenter(scheme, testNodePoolGVK)

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

func karpenterEnabledSettings(mode string, stacks ...string) []client.Object {
	objs := []client.Object{
		setting("enabled", "karpenter.enabled", "true", stacks...),
		setting("ec2", "karpenter.ec2-node-class.reference", "default", stacks...),
		setting("np", "karpenter.node-pool.reference", "default", stacks...),
	}
	if mode != "" {
		objs = append(objs, setting("isolation", "karpenter.isolation", mode, stacks...))
	}
	return objs
}

func stackWithUID(name, uid string, labels map[string]string) *v1beta1.Stack {
	s := stackWithLabels(name, labels)
	s.SetUID(types.UID(uid))
	return s
}

func getUnstructured(t *testing.T, ctx *mockContext, gvk schema.GroupVersionKind, name string) (*unstructured.Unstructured, bool) {
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: name}, obj)
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	require.NoError(t, err)
	return obj, true
}

func TestReconcileNoopWhenUnavailable(t *testing.T) {
	SetAvailable(false)
	ctx := newKarpenterContext(t, karpenterEnabledSettings("", "*")...)
	require.NoError(t, Reconcile(ctx, stackWithUID("stack0", "uid-0",
		map[string]string{v1beta1.OrganizationLabel: "acme"})))
	_, found := getUnstructured(t, ctx, testEC2GVK, "acme")
	require.False(t, found)
}

func TestReconcileCreatesOrganizationPool(t *testing.T) {
	SetAvailable(true)
	t.Cleanup(func() { SetAvailable(false) })

	objs := append(karpenterEnabledSettings("", "*"),
		reference(testEC2GVK, "default", map[string]any{"role": "KarpenterNodeRole"}),
		reference(testNodePoolGVK, "default", map[string]any{
			"template": map[string]any{"spec": map[string]any{}},
		}),
	)
	ctx := newKarpenterContext(t, objs...)
	stack := stackWithUID("stack0", "uid-0", map[string]string{
		v1beta1.OrganizationLabel: "acme",
		v1beta1.CustomerLabel:     "acme-corp",
	})

	require.NoError(t, Reconcile(ctx, stack))

	ec2, found := getUnstructured(t, ctx, testEC2GVK, "acme")
	require.True(t, found)
	tags, _, _ := unstructured.NestedStringMap(ec2.Object, "spec", "tags")
	require.Equal(t, "acme-corp", tags[v1beta1.CustomerLabel])
	require.Equal(t, "acme", tags[v1beta1.OrganizationLabel])
	require.NotContains(t, tags, v1beta1.StackLabel)
	// Reference field is preserved by the clone.
	role, _, _ := unstructured.NestedString(ec2.Object, "spec", "role")
	require.Equal(t, "KarpenterNodeRole", role)
	require.Equal(t, ManagedByValue, ec2.GetLabels()[ManagedByLabel])
	require.Len(t, ec2.GetOwnerReferences(), 1)

	np, found := getUnstructured(t, ctx, testNodePoolGVK, "acme")
	require.True(t, found)
	nodeLabels, _, _ := unstructured.NestedStringMap(np.Object, "spec", "template", "metadata", "labels")
	require.Equal(t, "acme", nodeLabels[v1beta1.OrganizationLabel])
	nodeClassName, _, _ := unstructured.NestedString(np.Object, "spec", "template", "spec", "nodeClassRef", "name")
	require.Equal(t, "acme", nodeClassName)
	taints, _, _ := unstructured.NestedSlice(np.Object, "spec", "template", "spec", "taints")
	require.Len(t, taints, 1) // organization only in org mode (customer is not a scheduling key)
}

func TestReconcileGCOnModeChange(t *testing.T) {
	SetAvailable(true)
	t.Cleanup(func() { SetAvailable(false) })

	// Start in organization mode → pool "acme".
	objs := append(karpenterEnabledSettings(ModeOrganization, "stack0"),
		reference(testEC2GVK, "default", map[string]any{"role": "r"}),
		reference(testNodePoolGVK, "default", map[string]any{"template": map[string]any{"spec": map[string]any{}}}),
	)
	ctx := newKarpenterContext(t, objs...)
	stack := stackWithUID("stack0", "uid-0", map[string]string{v1beta1.OrganizationLabel: "acme"})
	require.NoError(t, Reconcile(ctx, stack))
	_, found := getUnstructured(t, ctx, testEC2GVK, "acme")
	require.True(t, found)

	// Switch to stack mode → new pool "acme-stack0"; old "acme" (owned only by this stack)
	// must be released and deleted.
	isolation := &v1beta1.Settings{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "isolation"}, isolation))
	isolation.Spec.Value = ModeStack
	require.NoError(t, ctx.GetClient().Update(ctx, isolation))
	require.NoError(t, Reconcile(ctx, stack))

	_, found = getUnstructured(t, ctx, testEC2GVK, "acme-stack0")
	require.True(t, found, "new stack-mode pool should exist")
	_, found = getUnstructured(t, ctx, testEC2GVK, "acme")
	require.False(t, found, "old organization pool should be garbage collected")
	_, found = getUnstructured(t, ctx, testNodePoolGVK, "acme")
	require.False(t, found, "old organization NodePool should be garbage collected")
}

func TestReconcileGCOnDisable(t *testing.T) {
	SetAvailable(true)
	t.Cleanup(func() { SetAvailable(false) })

	objs := append(karpenterEnabledSettings(ModeOrganization, "stack0"),
		reference(testEC2GVK, "default", map[string]any{"role": "r"}),
		reference(testNodePoolGVK, "default", map[string]any{"template": map[string]any{"spec": map[string]any{}}}),
	)
	ctx := newKarpenterContext(t, objs...)
	stack := stackWithUID("stack0", "uid-0", map[string]string{v1beta1.OrganizationLabel: "acme"})
	require.NoError(t, Reconcile(ctx, stack))
	_, found := getUnstructured(t, ctx, testEC2GVK, "acme")
	require.True(t, found)

	// Disable karpenter → the cluster-scoped pool must be cleaned up (the regression the
	// review flagged: module cleanup cannot see cluster-scoped objects).
	disabled := &v1beta1.Settings{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "enabled"}, disabled))
	disabled.Spec.Value = "false"
	require.NoError(t, ctx.GetClient().Update(ctx, disabled))

	require.NoError(t, Reconcile(ctx, stack))
	_, found = getUnstructured(t, ctx, testEC2GVK, "acme")
	require.False(t, found)
	_, found = getUnstructured(t, ctx, testNodePoolGVK, "acme")
	require.False(t, found)
}

func TestReconcileOrgPoolSurvivesWhileAnotherStackOwns(t *testing.T) {
	SetAvailable(true)
	t.Cleanup(func() { SetAvailable(false) })

	objs := append(karpenterEnabledSettings(ModeOrganization, "*"),
		reference(testEC2GVK, "default", map[string]any{"role": "r"}),
		reference(testNodePoolGVK, "default", map[string]any{"template": map[string]any{"spec": map[string]any{}}}),
	)
	ctx := newKarpenterContext(t, objs...)

	stackA := stackWithUID("stackA", "uid-a", map[string]string{v1beta1.OrganizationLabel: "acme"})
	stackB := stackWithUID("stackB", "uid-b", map[string]string{v1beta1.OrganizationLabel: "acme"})
	require.NoError(t, Reconcile(ctx, stackA))
	require.NoError(t, Reconcile(ctx, stackB))

	ec2, found := getUnstructured(t, ctx, testEC2GVK, "acme")
	require.True(t, found)
	require.Len(t, ec2.GetOwnerReferences(), 2, "both stacks should own the shared org pool")

	// Disable for stackA only → pool must survive because stackB still owns it.
	require.NoError(t, ctx.GetClient().Create(ctx, setting("disableA", "karpenter.enabled", "false", "stackA")))
	require.NoError(t, Reconcile(ctx, stackA))

	ec2, found = getUnstructured(t, ctx, testEC2GVK, "acme")
	require.True(t, found, "org pool must survive while stackB owns it")
	require.Len(t, ec2.GetOwnerReferences(), 1)
}
