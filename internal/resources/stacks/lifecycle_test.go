package stacks

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func TestStackDeletionDoesNotSerializeModulesBehindConnectivity(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		for _, finalizers := range [][]string{nil, {"delete-ledger-credentials"}} {
			stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}, Spec: v1beta1.StackSpec{Disabled: disabled}}
			connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "connectivity", Finalizers: finalizers}, Spec: v1beta1.ConnectivitySpec{StackDependency: v1beta1.StackDependency{Stack: stack.Name}}}
			ledger := &v1beta1.Ledger{ObjectMeta: metav1.ObjectMeta{Name: "ledger", Finalizers: []string{"test-hold"}}, Spec: v1beta1.LedgerSpec{StackDependency: v1beta1.StackDependency{Stack: stack.Name}}}
			ctx := newNetworkPolicyMockContext(t, stack, connectivity, ledger)
			require.True(t, core.IsApplicationError(deleteModules(ctx, stack, logr.Discard())))
			require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), ledger))
			require.False(t, ledger.DeletionTimestamp.IsZero(), "Ledger deletion must be requested without waiting for Connectivity")
		}
	}
}
