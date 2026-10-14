package stacks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

// networkPolicyMockContext is a core.Context backed by a fake client and a real
// scheme, so createNetworkPolicies can render policies without a live cluster.
type networkPolicyMockContext struct {
	context.Context
	client client.Client
	scheme *runtime.Scheme
}

func (c *networkPolicyMockContext) GetClient() client.Client    { return c.client }
func (c *networkPolicyMockContext) GetScheme() *runtime.Scheme  { return c.scheme }
func (c *networkPolicyMockContext) GetAPIReader() client.Reader { return c.client }
func (c *networkPolicyMockContext) GetPlatform() core.Platform  { return core.Platform{} }

func newNetworkPolicyMockContext(t *testing.T, objects ...client.Object) core.Context {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, networkingv1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))
	kubernetesClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
	return &networkPolicyMockContext{
		Context: context.Background(),
		scheme:  scheme,
		client:  kubernetesClient,
	}
}

func loadNetworkPolicy(t *testing.T, ctx core.Context, namespace, name string) *networkingv1.NetworkPolicy {
	t.Helper()
	np := &networkingv1.NetworkPolicy{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, np))
	return np
}

func TestCreateNetworkPolicies_IsolatesStackIngress(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "test-stack", UID: "stack-uid"}}
	ctx := newNetworkPolicyMockContext(t)
	require.NoError(t, createNetworkPolicies(ctx, stack))

	local := loadNetworkPolicy(t, ctx, stack.Name, "allow-same-namespace")
	require.Equal(t, metav1.LabelSelector{}, local.Spec.PodSelector)
	require.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, local.Spec.PolicyTypes)
	require.Len(t, local.Spec.Ingress, 1)
	require.Empty(t, local.Spec.Ingress[0].Ports)
	require.Len(t, local.Spec.Ingress[0].From, 1)
	peer := local.Spec.Ingress[0].From[0]
	// An empty namespace selector would allow every namespace. It must be nil.
	require.Nil(t, peer.NamespaceSelector)
	require.Equal(t, &metav1.LabelSelector{}, peer.PodSelector)
	require.Nil(t, peer.IPBlock)
	require.Empty(t, local.Spec.Egress)
	require.True(t, metav1.IsControlledBy(local, stack))

	gateway := loadNetworkPolicy(t, ctx, stack.Name, "allow-gateway-ingress")
	require.Equal(t, metav1.LabelSelector{MatchLabels: map[string]string{
		"app.kubernetes.io/name": "gateway",
	}}, gateway.Spec.PodSelector)
	require.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, gateway.Spec.PolicyTypes)
	require.Len(t, gateway.Spec.Ingress, 1)
	require.Empty(t, gateway.Spec.Ingress[0].From)
	require.Len(t, gateway.Spec.Ingress[0].Ports, 1)
	require.Equal(t, "TCP", string(*gateway.Spec.Ingress[0].Ports[0].Protocol))
	require.Equal(t, 8080, gateway.Spec.Ingress[0].Ports[0].Port.IntValue())
	require.Empty(t, gateway.Spec.Egress)
	require.True(t, metav1.IsControlledBy(gateway, stack))

	policies := &networkingv1.NetworkPolicyList{}
	require.NoError(t, ctx.GetClient().List(ctx, policies, client.InNamespace(stack.Name)))
	require.Len(t, policies.Items, 2)
	// Repeated reconciliation is idempotent.
	require.NoError(t, createNetworkPolicies(ctx, stack))
	require.NoError(t, ctx.GetClient().List(ctx, policies, client.InNamespace(stack.Name)))
	require.Len(t, policies.Items, 2)
}

func TestNetworkPolicies_UpgradeAndDisable(t *testing.T) {
	historicalNames := []string{
		"default-deny-ingress", "allow-from-gateway", "allow-ledger-v3-cluster",
		"allow-ledger-v3-preview-cluster", "allow-ledger-v2-from-v3", "allow-ledger-v3-from-connectivity",
	}
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "upgrade", false: "disable"}[enabled], func(t *testing.T) {
			stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "test-stack", UID: "stack-uid"}}
			var objects []client.Object
			// Include every historical rule, the existing unrestricted Gateway rule,
			// and the new local rule to cover both upgrade and disable reconciliation.
			names := append([]string{"allow-same-namespace", "allow-gateway-ingress"}, historicalNames...)
			for _, name := range names {
				objects = append(objects, &networkingv1.NetworkPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: stack.Name},
					Spec: networkingv1.NetworkPolicySpec{
						PodSelector: metav1.LabelSelector{},
						PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
						Ingress:     []networkingv1.NetworkPolicyIngressRule{{}},
					},
				})
			}
			unrelated := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "custom-egress", Namespace: stack.Name},
				Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}}}
			otherStack := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-from-gateway", Namespace: "other-stack"}}
			objects = append(objects, unrelated, otherStack)
			ctx := newNetworkPolicyMockContext(t, objects...)
			if enabled {
				require.NoError(t, createNetworkPolicies(ctx, stack))
				gateway := loadNetworkPolicy(t, ctx, stack.Name, "allow-gateway-ingress")
				require.Len(t, gateway.Spec.Ingress[0].Ports, 1)
				require.Equal(t, 8080, gateway.Spec.Ingress[0].Ports[0].Port.IntValue())
				local := loadNetworkPolicy(t, ctx, stack.Name, "allow-same-namespace")
				require.Len(t, local.Spec.Ingress[0].From, 1)
			} else {
				require.NoError(t, deleteNetworkPolicies(ctx, stack))
				require.NoError(t, deleteNetworkPolicies(ctx, stack))
			}
			for _, name := range historicalNames {
				err := ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: name}, &networkingv1.NetworkPolicy{})
				require.True(t, apierrors.IsNotFound(err), "legacy policy %s remains: %v", name, err)
			}
			policies := &networkingv1.NetworkPolicyList{}
			require.NoError(t, ctx.GetClient().List(ctx, policies, client.InNamespace(stack.Name)))
			want := 1
			if enabled {
				want = 3
			}
			require.Len(t, policies.Items, want)
			require.Equal(t, unrelated.Spec, loadNetworkPolicy(t, ctx, stack.Name, unrelated.Name).Spec)
			require.Equal(t, otherStack.Spec, loadNetworkPolicy(t, ctx, otherStack.Namespace, otherStack.Name).Spec)
		})
	}
}
