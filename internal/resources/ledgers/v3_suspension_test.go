package ledgers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type suspensionTestContext struct {
	context.Context
	client client.Client
	scheme *runtime.Scheme
}

func (c suspensionTestContext) GetClient() client.Client    { return c.client }
func (c suspensionTestContext) GetAPIReader() client.Reader { return c.client }
func (c suspensionTestContext) GetScheme() *runtime.Scheme  { return c.scheme }
func (c suspensionTestContext) GetPlatform() core.Platform  { return core.Platform{} }

func TestDisabledV3LedgerKeepsClusterUntilSuspended(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = previous })
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	scheme.AddKnownTypeWithName(ledgerV3ClusterGVK, &unstructured.Unstructured{})
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack"}, Spec: v1beta1.StackSpec{Disabled: true}}
	ledger := &v1beta1.Ledger{ObjectMeta: metav1.ObjectMeta{Name: "ledger"}}
	cluster := newV3Cluster()
	cluster.SetName("stack")
	cluster.SetNamespace("stack")
	cluster.Object["spec"] = map[string]any{"clusterID": "original-id", "replicas": int64(3)}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	ctx := suspensionTestContext{Context: t.Context(), client: c, scheme: scheme}
	options := &core.ReconcilerOptions[*v1beta1.Ledger]{Owns: map[client.Object][]builder.OwnsOption{}}

	err := disableLedger(ctx, stack, options, ledger)
	require.Error(t, err)
	got, exists, err := getV3Cluster(ctx, stack)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "original-id", got.Object["spec"].(map[string]any)["clusterID"])
	suspended, _, err := unstructured.NestedBool(got.Object, "spec", "suspend")
	require.NoError(t, err)
	require.True(t, suspended)

	got.Object["status"] = map[string]any{"phase": "Suspended", "observedGeneration": got.GetGeneration()}
	require.NoError(t, c.Update(ctx, got))
	require.NoError(t, disableLedger(ctx, stack, options, ledger))
	got, _, err = getV3Cluster(ctx, stack)
	require.NoError(t, err)
	require.Equal(t, "original-id", got.Object["spec"].(map[string]any)["clusterID"])
}

func TestResumeV3ClusterOnlyClearsSuspendFlag(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = previous })
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(ledgerV3ClusterGVK, &unstructured.Unstructured{})
	cluster := newV3Cluster()
	cluster.SetName("stack")
	cluster.SetNamespace("stack")
	cluster.Object["spec"] = map[string]any{"suspend": true, "clusterID": "original-id", "replicas": int64(3)}
	cluster.Object["status"] = map[string]any{"phase": "Suspended", "suspension": map[string]any{"phase": "Suspended"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	ctx := suspensionTestContext{Context: t.Context(), client: c, scheme: scheme}
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack"}}
	waiting, err := resumeV3Cluster(ctx, stack, true)
	require.True(t, waiting)
	require.Error(t, err)
	got, _, err := getV3Cluster(ctx, stack)
	require.NoError(t, err)
	suspend, _, err := unstructured.NestedBool(got.Object, "spec", "suspend")
	require.NoError(t, err)
	require.False(t, suspend)
	require.Equal(t, "original-id", got.Object["spec"].(map[string]any)["clusterID"])
}
