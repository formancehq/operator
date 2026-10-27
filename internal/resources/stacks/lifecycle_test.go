package stacks

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func TestStackDeletionWaitsForConnectivityBeforeOtherModules(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "enabled", true: "disabled"}[disabled], func(t *testing.T) {
			stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}, Spec: v1beta1.StackSpec{Disabled: disabled}}
			connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0-connectivity", UID: "connectivity-uid", Finalizers: []string{"delete-ledger-credentials"}}, Spec: v1beta1.ConnectivitySpec{StackDependency: v1beta1.StackDependency{Stack: stack.Name}}}
			ledger := &v1beta1.Ledger{ObjectMeta: metav1.ObjectMeta{Name: "stack0-ledger", Finalizers: []string{"test-hold"}}, Spec: v1beta1.LedgerSpec{StackDependency: v1beta1.StackDependency{Stack: stack.Name}}}
			foreign := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "other-connectivity"}, Spec: v1beta1.ConnectivitySpec{StackDependency: v1beta1.StackDependency{Stack: "other"}}}
			ctx := newNetworkPolicyMockContext(t, stack, connectivity, ledger, foreign)
			for range 2 {
				require.True(t, core.IsApplicationError(deleteModules(ctx, stack, logr.Discard())))
				require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), ledger))
				require.True(t, ledger.DeletionTimestamp.IsZero(), "Ledger deletion must wait")
				require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(connectivity), connectivity))
				require.False(t, connectivity.DeletionTimestamp.IsZero())
				require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(foreign), foreign))
				require.True(t, foreign.DeletionTimestamp.IsZero())
			}
			connectivity.Finalizers = nil
			require.NoError(t, ctx.GetClient().Update(ctx, connectivity))
			require.True(t, core.IsApplicationError(deleteModules(ctx, stack, logr.Discard())))
			require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), ledger))
			require.False(t, ledger.DeletionTimestamp.IsZero())
			ledger.Finalizers = nil
			require.NoError(t, ctx.GetClient().Update(ctx, ledger))
			require.NoError(t, deleteModules(ctx, stack, logr.Discard()))
			require.True(t, apierrors.IsNotFound(ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), ledger)))
		})
	}
}

func TestStackDeletionWaitsForConnectivityFinalizerInstallation(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "new-connectivity"}, Spec: v1beta1.ConnectivitySpec{StackDependency: v1beta1.StackDependency{Stack: stack.Name}}}
	ctx := newNetworkPolicyMockContext(t, stack, module)
	require.True(t, core.IsApplicationError(deleteModules(ctx, stack, logr.Discard())))
	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(module), module))
	require.True(t, module.DeletionTimestamp.IsZero(), "must not delete a module before its controller can protect cleanup")
}

func TestStackDeletionInventoriesEveryModuleBeforeDeleting(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	ledger := &v1beta1.Ledger{ObjectMeta: metav1.ObjectMeta{Name: "ledger", UID: "ledger-uid"}, Spec: v1beta1.LedgerSpec{StackDependency: v1beta1.StackDependency{Stack: stack.Name}}}
	for _, scenario := range []string{"late-connectivity", "failed-list"} {
		t.Run(scenario, func(t *testing.T) {
			base := newNetworkPolicyMockContext(t, stack, ledger).(*networkPolicyMockContext)
			sawConnectivity := false
			base.client = interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
				List: func(c context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if list.GetObjectKind().GroupVersionKind() == v1beta1.GroupVersion.WithKind("Connectivity") {
						sawConnectivity = true
						if scenario == "failed-list" {
							return errors.New("inventory unavailable")
						}
						module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "late-connectivity", UID: "module-uid"}, Spec: v1beta1.ConnectivitySpec{StackDependency: v1beta1.StackDependency{Stack: stack.Name}}}
						require.NoError(t, cl.Create(c, module))
					}
					return cl.List(c, list, opts...)
				},
				Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					t.Fatal("no deletion before complete inventory and Connectivity finalizer protection")
					return nil
				},
			})
			require.Error(t, deleteModules(base, stack, logr.Discard()))
			require.True(t, sawConnectivity)
			got := &v1beta1.Ledger{}
			require.NoError(t, base.client.Get(base, client.ObjectKeyFromObject(ledger), got))
			require.True(t, got.DeletionTimestamp.IsZero())
		})
	}
}
