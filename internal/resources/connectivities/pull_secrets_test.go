package connectivities

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
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
		require.True(t, metav1.IsControlledBy(ref, stack))
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
	// Removing registry credentials retains Core/API refs for other consumers.
	plugin := &v1beta1.ResourceReference{ObjectMeta: metav1.ObjectMeta{Name: "plugin", Labels: map[string]string{resourcereferences.ManagedPullSecretLabel: "true"}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(stack, v1beta1.GroupVersion.WithKind("Stack"))}}}
	require.NoError(t, ctx.client.Create(ctx, plugin))
	registry.Spec.Value = "mirror.example"
	require.NoError(t, ctx.client.Update(ctx, registry))
	require.Error(t, Reconcile(ctx, stack, module, "v1.0.0"))
	require.NoError(t, ctx.client.List(ctx, refs))
	require.Len(t, refs.Items, 3)
}

func TestCoreReferencesRetainedForStackLifetime(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant", UID: "stack"}}
	module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "connectivity", UID: "module"}}
	module.Spec.Stack = stack.Name
	ledger, credentials := newReadyLedgerPrerequisites()
	ledger.Spec.Stack = stack.Name
	auth := &v1beta1.Auth{ObjectMeta: metav1.ObjectMeta{Name: "auth"}}
	auth.Spec.Stack = stack.Name
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default", Labels: map[string]string{v1beta1.StackLabel: stack.Name}}, Data: map[string][]byte{"credential": []byte("initial")}}
	ctx := newReconcileTestContext(t, stack, module, ledger, credentials, auth, source)
	secrets := []corev1.LocalObjectReference{{Name: source.Name}}
	require.Error(t, reconcileImagePullSecretReferences(ctx, stack, secrets, secrets))
	list := &v1beta1.ResourceReferenceList{}
	require.NoError(t, ctx.client.List(ctx, list))
	require.Len(t, list.Items, 1)
	ref := &list.Items[0]
	ref.UID = "stable-reference"
	require.NoError(t, ctx.client.Update(ctx, ref))
	require.True(t, metav1.IsControlledBy(ref, stack))
	require.False(t, metav1.IsControlledBy(ref, module))
	require.Len(t, ref.OwnerReferences, 1) // Stack GC, independent of module lifetime.
	require.NoError(t, resourcereferences.Reconcile(ctx, stack, ref))
	ref.Status.Ready = true
	require.NoError(t, ctx.client.Update(ctx, ref))
	name, uid := ref.Name, ref.UID
	require.NoError(t, ctx.client.Delete(ctx, module))
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(ledger), &v1beta1.Ledger{}))
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(auth), &v1beta1.Auth{}))
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKey{Name: name}, ref))
	source.Data["credential"] = []byte("rotated")
	require.NoError(t, ctx.client.Update(ctx, source))
	require.NoError(t, resourcereferences.Reconcile(ctx, stack, ref))
	copy := &corev1.Secret{}
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: source.Name}, copy))
	require.Equal(t, source.Data, copy.Data)
	require.True(t, metav1.IsControlledBy(ref, stack))
	require.Equal(t, uid, copy.OwnerReferences[0].UID)
	module = &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "connectivity", UID: "replacement-module"}}
	module.Spec.Stack = stack.Name
	require.NoError(t, ctx.client.Create(ctx, module))
	require.NoError(t, reconcileImagePullSecretReferences(ctx, stack, secrets, nil))
	require.NoError(t, ctx.client.List(ctx, list))
	require.Len(t, list.Items, 1)
	require.Equal(t, name, list.Items[0].Name)
	require.Equal(t, uid, list.Items[0].UID)
	require.NoError(t, reconcileImagePullSecretReferences(ctx, stack, nil, nil))
	require.NoError(t, ctx.client.List(ctx, list))
	require.Len(t, list.Items, 1) // Dropping configured names does not prune shared credentials.
	require.Equal(t, uid, list.Items[0].UID)
}

func TestStackOwnedPullSecretReferenceMapsToConnectivity(t *testing.T) {
	module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "actual-module"}}
	module.Spec.Stack = "tenant"
	other := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "other-module"}}
	other.Spec.Stack = "other"
	ctx := newReconcileTestContext(t, module, other)
	ref := &v1beta1.ResourceReference{}
	ref.Spec.Stack = "tenant"
	requests := mapPullSecretReferenceToConnectivity(ctx, ref)
	require.Len(t, requests, 1)
	require.Equal(t, module.Name, requests[0].Name)
	ref.Spec.Stack = "absent"
	require.Empty(t, mapPullSecretReferenceToConnectivity(ctx, ref))
	ref.Spec.Stack = ""
	require.Empty(t, mapPullSecretReferenceToConnectivity(ctx, ref))
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

func TestStackOwnedPullSecretStatusEventEnqueuesConnectivity(t *testing.T) {
	module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "actual-module"}}
	module.Spec.Stack = "tenant"
	ctx := newReconcileTestContext(t, module)
	manager := connectivityWatchTestManager{client: ctx.client, scheme: ctx.scheme}
	options := core.ReconcilerOptions[*v1beta1.Connectivity]{Owns: map[client.Object][]builder.OwnsOption{}, Watchers: map[client.Object]core.ReconcilerOptionsWatch{}}
	for _, option := range connectivityReconcilerOptions() {
		option(&options)
	}
	var watch core.ReconcilerOptionsWatch
	found := false
	for object, candidate := range options.Watchers {
		if _, ok := object.(*v1beta1.ResourceReference); ok {
			watch, found = candidate, true
		}
	}
	require.True(t, found, "Stack ownership requires an explicit ResourceReference watch")
	handler, _ := watch.Handler(manager, nil, module)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: module.Spec.Stack, UID: "stack"}}
	before := &v1beta1.ResourceReference{ObjectMeta: metav1.ObjectMeta{Name: "shared-reference", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(stack, v1beta1.GroupVersion.WithKind("Stack"))}}}
	before.Spec.Stack = stack.Name
	after := before.DeepCopy()
	after.Status.Ready = true
	handler.Update(context.Background(), event.UpdateEvent{ObjectOld: before, ObjectNew: after}, queue)
	require.Equal(t, 1, queue.Len())
	request, _ := queue.Get()
	defer queue.Done(request)
	require.Equal(t, module.Name, request.Name)
}
