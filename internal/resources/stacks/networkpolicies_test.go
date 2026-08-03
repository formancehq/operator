package stacks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
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

func newNetworkPolicyMockContext(t *testing.T) core.Context {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, networkingv1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))
	return &networkPolicyMockContext{
		Context: context.Background(),
		scheme:  scheme,
		client:  fake.NewClientBuilder().WithScheme(scheme).Build(),
	}
}

func loadNetworkPolicy(t *testing.T, ctx core.Context, namespace, name string) *networkingv1.NetworkPolicy {
	t.Helper()
	np := &networkingv1.NetworkPolicy{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, np))
	return np
}

// TestCreateNetworkPolicies_AllowsConnectivityToLedgerV3 asserts that the
// connectivity workload is granted ingress to the Ledger v3 pods, so its
// delegated pods are not blocked by the default-deny policy.
func TestCreateNetworkPolicies_AllowsConnectivityToLedgerV3(t *testing.T) {
	ctx := newNetworkPolicyMockContext(t)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "test-stack", Generation: 1}}

	require.NoError(t, createNetworkPolicies(ctx, stack))

	np := loadNetworkPolicy(t, ctx, stack.Name, "allow-ledger-v3-from-connectivity")

	// Target pods: the stack's Ledger v3 replicas.
	require.Equal(t, map[string]string{
		"app.kubernetes.io/instance": stack.Name,
		"app.kubernetes.io/name":     "ledger",
	}, np.Spec.PodSelector.MatchLabels)
	require.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)

	// A single ingress rule allowing the connectivity workload in.
	require.Len(t, np.Spec.Ingress, 1)
	require.Len(t, np.Spec.Ingress[0].From, 1)

	peer := np.Spec.Ingress[0].From[0]
	// Same-namespace pod selector (no namespace selector): connectivity runs in
	// the stack namespace alongside the ledger.
	require.Nil(t, peer.NamespaceSelector)
	require.NotNil(t, peer.PodSelector)
	require.Equal(t, "connectivity", peer.PodSelector.MatchLabels["app.kubernetes.io/name"])

	// Ports are intentionally unrestricted for this tightly scoped
	// connectivity->ledger-v3 pair (mirroring allow-ledger-v3-cluster), so a
	// LedgerConfiguration grpcPort override can never break or stale the policy.
	require.Empty(t, np.Spec.Ingress[0].Ports)
}

func TestConnectivitySelector(t *testing.T) {
	require.Equal(t, map[string]string{
		"app.kubernetes.io/name": "connectivity",
	}, connectivitySelector().MatchLabels)
}
