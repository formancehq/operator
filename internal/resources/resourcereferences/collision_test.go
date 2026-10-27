package resourcereferences

import (
	"context"
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
)

type collisionContext struct {
	context.Context
	client client.Client
	scheme *runtime.Scheme
}

func (c *collisionContext) GetClient() client.Client    { return c.client }
func (c *collisionContext) GetAPIReader() client.Reader { return c.client }
func (c *collisionContext) GetScheme() *runtime.Scheme  { return c.scheme }
func (c *collisionContext) GetPlatform() core.Platform  { return core.Platform{} }

func newCollisionContext(t *testing.T, objects ...client.Object) *collisionContext {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))
	return &collisionContext{Context: context.Background(), scheme: scheme,
		client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
}

func registryReference() *v1beta1.ResourceReference {
	return &v1beta1.ResourceReference{
		ObjectMeta: metav1.ObjectMeta{Name: "registry-reference", UID: "reference-uid",
			Labels: map[string]string{ManagedPullSecretLabel: "true"}},
		Spec: v1beta1.ResourceReferenceSpec{
			StackDependency: v1beta1.StackDependency{Stack: "tenant"},
			Name:            "pull-secret", GroupVersionKind: &metav1.GroupVersionKind{Version: "v1", Kind: "Secret"},
		},
	}
}

func TestRegistryCopyCollision(t *testing.T) {
	for _, tc := range []struct {
		name      string
		owner     *metav1.OwnerReference
		liveOwner bool
		generic   bool
		allowed   bool
	}{
		{name: "unmanaged local secret"},
		{name: "unrelated owner", owner: &metav1.OwnerReference{APIVersion: "v1", Kind: "ServiceAccount", Name: "other", UID: "other-uid"}},
		{name: "missing reference", owner: &metav1.OwnerReference{APIVersion: v1beta1.GroupVersion.String(), Kind: "ResourceReference", Name: "other", UID: "other-uid"}},
		{name: "stale UID", liveOwner: true, owner: &metav1.OwnerReference{APIVersion: v1beta1.GroupVersion.String(), Kind: "ResourceReference", Name: "other", UID: "stale-uid"}},
		{name: "empty UID", liveOwner: true, owner: &metav1.OwnerReference{APIVersion: v1beta1.GroupVersion.String(), Kind: "ResourceReference", Name: "other"}},
		{name: "shared generic reference", liveOwner: true, allowed: true, owner: &metav1.OwnerReference{APIVersion: v1beta1.GroupVersion.String(), Kind: "ResourceReference", Name: "other", UID: "other-uid"}},
		{name: "ordinary reference unchanged", generic: true, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reference := registryReference()
			if tc.generic {
				reference.Labels = nil
			}
			source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "source",
				Labels: map[string]string{v1beta1.StackLabel: "any"}}, Data: map[string][]byte{"credential": []byte("new")}}
			target := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "tenant", UID: "local-uid"},
				Data: map[string][]byte{"credential": []byte("local")}}
			if tc.owner != nil {
				target.OwnerReferences = []metav1.OwnerReference{*tc.owner}
			}
			objects := []client.Object{source, target, reference}
			if tc.liveOwner {
				other := registryReference()
				other.Name, other.UID, other.Labels = "other", "other-uid", nil
				objects = append(objects, other)
			}
			ctx := newCollisionContext(t, objects...)
			before := &corev1.Secret{}
			key := types.NamespacedName{Namespace: "tenant", Name: "pull-secret"}
			require.NoError(t, ctx.client.Get(ctx, key, before))
			err := Reconcile(ctx, &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant"}}, reference)
			after := &corev1.Secret{}
			require.NoError(t, ctx.client.Get(ctx, key, after))
			if !tc.allowed {
				require.ErrorContains(t, err, "refusing to overwrite unmanaged pull secret")
				require.Equal(t, before, after)
				require.Empty(t, reference.Status.SyncedResource)
				return
			}
			require.NoError(t, err)
			require.Equal(t, source.Data, after.Data)
			require.Contains(t, after.OwnerReferences, metav1.OwnerReference{APIVersion: v1beta1.GroupVersion.String(), Kind: "ResourceReference", Name: reference.Name, UID: reference.UID})
		})
	}
}

func TestRegistryCopyCreationAndRotation(t *testing.T) {
	reference := registryReference()
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "source",
		Labels: map[string]string{v1beta1.StackLabel: "tenant"}}, Data: map[string][]byte{"credential": []byte("initial")}}
	ctx := newCollisionContext(t, source, reference)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant"}}
	require.NoError(t, Reconcile(ctx, stack, reference))
	target := &corev1.Secret{}
	key := types.NamespacedName{Namespace: "tenant", Name: "pull-secret"}
	require.NoError(t, ctx.client.Get(ctx, key, target))
	require.Equal(t, source.Data, target.Data)
	require.NotContains(t, target.Labels, v1beta1.StackLabel)
	// The copied target cannot become an authorized source on the next reconciliation.
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(source), source))
	source.Data["credential"] = []byte("rotated")
	require.NoError(t, ctx.client.Update(ctx, source))
	require.NoError(t, Reconcile(ctx, stack, reference))
	require.NoError(t, ctx.client.Get(ctx, key, target))
	require.Equal(t, source.Data, target.Data)
	require.Len(t, target.OwnerReferences, 1)
}

func TestAliasedSourceChangesPreserveDestinationAndAuthorization(t *testing.T) {
	ref := registryReference()
	ref.Spec.TargetName = "fixed-target"
	ref.Generation = 1
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: ref.Spec.Name, Namespace: "source", Labels: map[string]string{v1beta1.StackLabel: "tenant"}}, Data: map[string][]byte{"credential": []byte("first")}}
	replacement := source.DeepCopy()
	replacement.Name = "replacement"
	replacement.Labels = nil
	replacement.Data = map[string][]byte{"credential": []byte("second")}
	ctx := newCollisionContext(t, ref, source, replacement)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant"}}
	require.NoError(t, Reconcile(ctx, stack, ref))
	require.Equal(t, "fixed-target", ref.Status.SyncedResource)
	require.Equal(t, int64(1), ref.Status.ObservedGeneration)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(ref), ref))
	ref.Spec.Name = replacement.Name
	ref.Generation = 2
	require.NoError(t, ctx.client.Update(ctx, ref))
	require.Error(t, Reconcile(ctx, stack, ref))
	target := &corev1.Secret{}
	key := types.NamespacedName{Name: "fixed-target", Namespace: "tenant"}
	require.NoError(t, ctx.client.Get(ctx, key, target))
	require.Equal(t, source.Data, target.Data)
	replacement.Labels = map[string]string{v1beta1.StackLabel: "tenant"}
	require.NoError(t, ctx.client.Update(ctx, replacement))
	require.NoError(t, Reconcile(ctx, stack, ref))
	require.NoError(t, ctx.client.Get(ctx, key, target))
	require.Equal(t, replacement.Data, target.Data)
	require.Equal(t, "fixed-target", ref.Status.SyncedResource)
	require.Equal(t, int64(2), ref.Status.ObservedGeneration)
}

func TestAliasDestinationRejectsIncompatibleWriters(t *testing.T) {
	for _, test := range []struct {
		name, source   string
		plain, allowed bool
	}{
		{name: "same mapping shares ownership", source: "first", allowed: true},
		{name: "different aliased source", source: "second"},
		{name: "plain reference cannot overwrite an alias", source: "fixed-target", plain: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			existing := registryReference()
			existing.Name = "existing"
			existing.UID = "existing-uid"
			existing.Spec.Name = "first"
			existing.Spec.TargetName = "fixed-target"
			desired := registryReference()
			desired.Spec.Name = test.source
			desired.Spec.TargetName = "fixed-target"
			if test.plain {
				desired.Labels = nil
				desired.Spec.TargetName = ""
			}
			source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: test.source, Namespace: "source", Labels: map[string]string{v1beta1.StackLabel: "any"}}, Data: map[string][]byte{"value": []byte("new")}}
			target := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "fixed-target", Namespace: "tenant", OwnerReferences: []metav1.OwnerReference{{APIVersion: v1beta1.GroupVersion.String(), Kind: "ResourceReference", Name: existing.Name, UID: existing.UID}}}, Data: map[string][]byte{"value": []byte("previous")}}
			ctx := newCollisionContext(t, existing, desired, source, target)
			err := Reconcile(ctx, &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant"}}, desired)
			got := &corev1.Secret{}
			require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(target), got))
			if test.allowed {
				require.NoError(t, err)
				require.Equal(t, source.Data, got.Data)
				require.Len(t, got.OwnerReferences, 2)
			} else {
				require.ErrorContains(t, err, "incompatible ResourceReference source/destination")
				require.Equal(t, target.Data, got.Data)
				require.Len(t, got.OwnerReferences, 1)
			}
		})
	}
}

func TestTargetRenameCopiesBeforeRemovingPreviousOwnership(t *testing.T) {
	ref := registryReference()
	ref.Spec.TargetName = "old-target"
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: ref.Spec.Name, Namespace: "source", Labels: map[string]string{v1beta1.StackLabel: "tenant"}}, Data: map[string][]byte{"credential": []byte("source")}}
	ctx := newCollisionContext(t, source, ref)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "tenant"}}
	require.NoError(t, Reconcile(ctx, stack, ref))
	require.Equal(t, "old-target", ref.Status.SyncedResource)
	local := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "new-target", Namespace: "tenant"}, Data: map[string][]byte{"credential": []byte("local")}}
	require.NoError(t, ctx.client.Create(ctx, local))
	ref.Spec.TargetName = "new-target"
	require.ErrorContains(t, Reconcile(ctx, stack, ref), "refusing to overwrite unmanaged pull secret")
	old := &corev1.Secret{}
	require.NoError(t, ctx.client.Get(ctx, types.NamespacedName{Name: "old-target", Namespace: "tenant"}, old))
	require.Len(t, old.OwnerReferences, 1)
	require.Equal(t, "old-target", ref.Status.SyncedResource)
	require.NoError(t, ctx.client.Delete(ctx, local))
	require.NoError(t, Reconcile(ctx, stack, ref))
	target := &corev1.Secret{}
	require.NoError(t, ctx.client.Get(ctx, types.NamespacedName{Name: "new-target", Namespace: "tenant"}, target))
	require.Equal(t, source.Data, target.Data)
	require.Error(t, ctx.client.Get(ctx, types.NamespacedName{Name: "old-target", Namespace: "tenant"}, old))
	require.Equal(t, "new-target", ref.Status.SyncedResource)
}

func TestDestinationWatchEnqueuesEveryReferenceOwner(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "aliased-target", Namespace: "tenant", OwnerReferences: []metav1.OwnerReference{
		{APIVersion: v1beta1.GroupVersion.String(), Kind: "ResourceReference", Name: "one", UID: "one"},
		{APIVersion: v1beta1.GroupVersion.String(), Kind: "ResourceReference", Name: "two", UID: "two"},
	}}}
	ctx := newCollisionContext(t)
	requests := watchResource(ctx, secret)
	require.Len(t, requests, 2)
	require.Equal(t, "one", requests[0].Name)
	require.Equal(t, "two", requests[1].Name)
}
