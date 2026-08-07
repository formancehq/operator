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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

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
		WithIndex(&v1beta1.Connectivity{}, "stack", func(object client.Object) []string {
			return []string{object.(*v1beta1.Connectivity).GetStack()}
		}).
		WithIndex(&v1beta1.LedgerConfiguration{}, "stack", func(object client.Object) []string {
			return object.(*v1beta1.LedgerConfiguration).GetStacks()
		}).
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

func TestCreateNetworkPolicies_OmitsConnectivityPolicyWithoutModule(t *testing.T) {
	ctx := newNetworkPolicyMockContext(t)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "test-stack", Generation: 1}}

	require.NoError(t, createNetworkPolicies(ctx, stack))

	np := &networkingv1.NetworkPolicy{}
	err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      "allow-ledger-v3-from-connectivity",
	}, np)
	require.True(t, apierrors.IsNotFound(err))
}

// TestCreateNetworkPolicies_AllowsConnectivityToLedgerV3 asserts that the
// connectivity workload is granted ingress to the Ledger v3 pods, so its
// delegated pods are not blocked by the default-deny policy.
func TestCreateNetworkPolicies_AllowsConnectivityToLedgerV3(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "test-stack", Generation: 1}}
	connectivity := &v1beta1.Connectivity{
		ObjectMeta: metav1.ObjectMeta{Name: stack.Name},
		Spec: v1beta1.ConnectivitySpec{
			StackDependency: v1beta1.StackDependency{Stack: stack.Name},
		},
	}
	ctx := newNetworkPolicyMockContext(t, connectivity)

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
	require.Equal(t, map[string]string{
		"app.kubernetes.io/instance": "connectivity",
		"app.kubernetes.io/name":     "connectivity",
	}, peer.PodSelector.MatchLabels)

	require.Len(t, np.Spec.Ingress[0].Ports, 1)
	require.NotNil(t, np.Spec.Ingress[0].Ports[0].Protocol)
	require.Equal(t, "TCP", string(*np.Spec.Ingress[0].Ports[0].Protocol))
	require.NotNil(t, np.Spec.Ingress[0].Ports[0].Port)
	require.Equal(t, 8888, np.Spec.Ingress[0].Ports[0].Port.IntValue())
}

func TestCreateNetworkPolicies_UsesConfiguredLedgerGRPCPort(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "test-stack", Generation: 1}}
	connectivity := &v1beta1.Connectivity{
		ObjectMeta: metav1.ObjectMeta{Name: stack.Name},
		Spec: v1beta1.ConnectivitySpec{
			StackDependency: v1beta1.StackDependency{Stack: stack.Name},
		},
	}
	configuration := &v1beta1.LedgerConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "test-stack"},
		Spec: v1beta1.LedgerConfigurationSpec{
			Stacks: []string{stack.Name},
			Cluster: ledgerv1alpha1.ClusterSpec{
				Service: ledgerv1alpha1.ServiceSpec{GrpcPort: 7777},
			},
		},
	}
	ctx := newNetworkPolicyMockContext(t, connectivity, configuration)

	require.NoError(t, createNetworkPolicies(ctx, stack))

	np := loadNetworkPolicy(t, ctx, stack.Name, "allow-ledger-v3-from-connectivity")
	require.Len(t, np.Spec.Ingress, 1)
	require.Len(t, np.Spec.Ingress[0].Ports, 1)
	require.Equal(t, 7777, np.Spec.Ingress[0].Ports[0].Port.IntValue())
}

func TestConnectivitySelector(t *testing.T) {
	require.Equal(t, map[string]string{
		"app.kubernetes.io/instance": "connectivity",
		"app.kubernetes.io/name":     "connectivity",
	}, connectivitySelector().MatchLabels)
}

func TestMapLedgerConfigurationToStacks(t *testing.T) {
	stackA := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack-a"}}
	stackB := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack-b"}}
	ctx := newNetworkPolicyMockContext(t, stackA, stackB)

	t.Run("specific stacks", func(t *testing.T) {
		configuration := &v1beta1.LedgerConfiguration{
			Spec: v1beta1.LedgerConfigurationSpec{Stacks: []string{stackA.Name}},
		}

		require.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Name: stackA.Name}}},
			mapLedgerConfigurationToStacks(ctx, configuration))
	})

	t.Run("wildcard", func(t *testing.T) {
		configuration := &v1beta1.LedgerConfiguration{
			Spec: v1beta1.LedgerConfigurationSpec{Stacks: []string{"*"}},
		}

		require.ElementsMatch(t, []reconcile.Request{
			{NamespacedName: types.NamespacedName{Name: stackA.Name}},
			{NamespacedName: types.NamespacedName{Name: stackB.Name}},
		}, mapLedgerConfigurationToStacks(ctx, configuration))
	})
}
