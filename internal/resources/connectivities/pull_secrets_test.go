package connectivities

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func TestExistingRegistrySecretsAreCopiedAndInjectedWithoutNewCoreSetting(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: "stack-uid"}}
	module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0-connectivity", UID: "module-uid"}}
	module.Spec.Stack = stack.Name
	ledger, credentials := newReadyLedgerPrerequisites()
	registry := settings.New("registry", "registries.*.endpoint", "mirror.example?pullSecret=core-secret,pullSecret=api-secret", stack.Name)
	sources := []client.Object{}
	for _, name := range []string{"core-secret", "api-secret"} {
		sources = append(sources, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: map[string]string{v1beta1.StackLabel: stack.Name}}, Data: map[string][]byte{"credential": []byte(name)}})
	}
	ctx := newReconcileTestContext(t, append([]client.Object{stack, module, ledger, credentials, registry}, sources...)...)
	require.Error(t, Reconcile(ctx, stack, module, "v1.0.0"))
	refs := &v1beta1.ResourceReferenceList{}
	require.NoError(t, ctx.client.List(ctx, refs))
	require.Len(t, refs.Items, 2)
	for i := range refs.Items {
		ref := &refs.Items[i]
		require.True(t, metav1.IsControlledBy(ref, module))
		require.Empty(t, ref.Spec.TargetName)
		require.NoError(t, resourcereferences.Reconcile(ctx, stack, ref))
		ref.Status.Ready = true
		require.NoError(t, ctx.client.Update(ctx, ref))
		copy := &corev1.Secret{}
		require.NoError(t, ctx.client.Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: ref.Spec.Name}, copy))
		require.Equal(t, []byte(ref.Spec.Name), copy.Data["credential"])
	}
	require.Error(t, Reconcile(ctx, stack, module, "v1.0.0")) // delegated workload readiness is still external
	delegated := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, delegated))
	want := []any{map[string]any{"name": "core-secret"}, map[string]any{"name": "api-secret"}}
	for _, path := range [][]string{{"spec", "imagePullSecrets"}, {"spec", "api", "imagePullSecrets"}} {
		got, _, err := unstructured.NestedSlice(delegated.Object, path...)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	image, _, err := unstructured.NestedString(delegated.Object, "spec", "image")
	require.NoError(t, err)
	require.Contains(t, image, "mirror.example/")
	// Removing registry credentials prunes this component's refs, not plugin refs.
	plugin := &v1beta1.ResourceReference{ObjectMeta: metav1.ObjectMeta{Name: "plugin", Labels: map[string]string{resourcereferences.ManagedPullSecretLabel: "true"}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(stack, v1beta1.GroupVersion.WithKind("Stack"))}}}
	require.NoError(t, ctx.client.Create(ctx, plugin))
	registry.Spec.Value = "mirror.example"
	require.NoError(t, ctx.client.Update(ctx, registry))
	require.Error(t, Reconcile(ctx, stack, module, "v1.0.0"))
	require.NoError(t, ctx.client.List(ctx, refs))
	require.Len(t, refs.Items, 1)
	require.Equal(t, "plugin", refs.Items[0].Name)
}

func TestCoreReferencesRetainedUntilReplacementCurrent(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant", UID: "stack"}}
	module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "tenant-connectivity", UID: "module"}}
	ctx := newReconcileTestContext(t, stack, module)
	secrets := []corev1.LocalObjectReference{{Name: "first"}}
	require.Error(t, reconcileImagePullSecretReferences(ctx, stack, module, secrets, secrets))
	list := &v1beta1.ResourceReferenceList{}
	require.NoError(t, ctx.client.List(ctx, list))
	require.Len(t, list.Items, 1)
	ref := &list.Items[0]
	ref.Status.Ready = true
	require.NoError(t, ctx.client.Update(ctx, ref))
	require.NoError(t, reconcileImagePullSecretReferences(ctx, stack, module, secrets, secrets))
	require.Error(t, reconcileImagePullSecretReferences(ctx, stack, module, []corev1.LocalObjectReference{{Name: "second"}}, nil))
	require.NoError(t, ctx.client.List(ctx, list))
	require.Len(t, list.Items, 2)
	for i := range list.Items {
		if list.Items[i].Spec.Name == "second" {
			list.Items[i].Status.Ready = true
			require.NoError(t, ctx.client.Update(ctx, &list.Items[i]))
		}
	}
	require.NoError(t, reconcileImagePullSecretReferences(ctx, stack, module, []corev1.LocalObjectReference{{Name: "second"}}, nil))
	require.NoError(t, ctx.client.List(ctx, list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "second", list.Items[0].Spec.Name)
}

func TestCoreSecretsBootstrapBeforeLedgerReady(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: "stack-uid"}}
	module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0-connectivity", UID: "module-uid"}}
	module.Spec.Stack = stack.Name
	ledger, credentials := newReadyLedgerPrerequisites()
	ledger.Status.Ready = false
	registry := settings.New("registry", "registries.*.endpoint", "mirror.example?pullSecret=private-images", stack.Name)
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "private-images", Namespace: "default", Labels: map[string]string{v1beta1.StackLabel: stack.Name}}, Data: map[string][]byte{"credential": []byte("private")}}
	ctx := newReconcileTestContext(t, stack, module, ledger, credentials, registry, source)
	require.Error(t, Reconcile(ctx, stack, module, "v1.0.0"))
	refs := &v1beta1.ResourceReferenceList{}
	require.NoError(t, ctx.client.List(ctx, refs))
	require.Len(t, refs.Items, 1)
	ref := &refs.Items[0]
	require.NoError(t, resourcereferences.Reconcile(ctx, stack, ref))
	copy := &corev1.Secret{}
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKey{Name: source.Name, Namespace: stack.Name}, copy))
	require.Equal(t, source.Data, copy.Data)
	ref.Status.Ready = true
	require.NoError(t, ctx.client.Update(ctx, ref))
	err := Reconcile(ctx, stack, module, "v1.0.0")
	require.ErrorContains(t, err, "waiting for the ledger to be ready")
	require.False(t, delegatedConnectivityExists(t, ctx, stack.Name))
}
