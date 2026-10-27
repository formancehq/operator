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
			Labels: map[string]string{RegistryPullSecretLabel: "true"}},
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
				require.ErrorContains(t, err, "refusing to overwrite unmanaged registry pull secret")
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
