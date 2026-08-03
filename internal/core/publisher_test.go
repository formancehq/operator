package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestListEventPublishersFiltersVersionedPublishers(t *testing.T) {
	tests := map[string]struct {
		reconciliationVersion string
		expectedServices      []string
	}{
		"v2.3 reconciliation is excluded": {
			reconciliationVersion: "v2.3.1",
			expectedServices:      []string{"Ledger", "Payments"},
		},
		"v2.4 prerelease reconciliation is excluded": {
			reconciliationVersion: "v2.4.0-rc.1",
			expectedServices:      []string{"Ledger", "Payments"},
		},
		"v2.4 reconciliation is included": {
			reconciliationVersion: "v2.4.0",
			expectedServices:      []string{"Ledger", "Payments", "Reconciliation"},
		},
		"main reconciliation is included": {
			reconciliationVersion: "main",
			expectedServices:      []string{"Ledger", "Payments", "Reconciliation"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, v1beta1.AddToScheme(scheme))

			stack := &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{Name: "stack"},
				Spec:       v1beta1.StackSpec{VersionsFromFile: "default"},
			}
			versions := &v1beta1.Versions{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
				Spec: map[string]string{
					"ledger":         "v2.3.1",
					"payments":       "v2.3.1",
					"reconciliation": tt.reconciliationVersion,
				},
			}
			ledger := &v1beta1.Ledger{
				ObjectMeta: metav1.ObjectMeta{Name: "stack"},
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				},
			}
			payments := &v1beta1.Payments{
				ObjectMeta: metav1.ObjectMeta{Name: "stack"},
				Spec: v1beta1.PaymentsSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				},
			}
			reconciliation := &v1beta1.Reconciliation{
				ObjectMeta: metav1.ObjectMeta{Name: "stack"},
				Spec: v1beta1.ReconciliationSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				},
			}

			stackIndex := func(object client.Object) []string {
				if value, found, err := unstructured.NestedString(object.(*unstructured.Unstructured).Object, "spec", "stack"); err == nil && found {
					return []string{value}
				}
				return nil
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(stack, versions, ledger, payments, reconciliation).
				WithIndex(&v1beta1.Ledger{}, "stack", stackIndex).
				WithIndex(&v1beta1.Payments{}, "stack", stackIndex).
				WithIndex(&v1beta1.Reconciliation{}, "stack", stackIndex).
				WithIndex(&v1beta1.Orchestration{}, "stack", stackIndex).
				WithIndex(&v1beta1.TransactionPlane{}, "stack", stackIndex).
				Build()

			ctx := testContext{
				Context:   context.Background(),
				client:    fakeClient,
				apiReader: fakeClient,
				scheme:    scheme,
			}
			publishers, err := ListEventPublishers(ctx, stack.Name)
			require.NoError(t, err)

			services := make([]string, 0, len(publishers))
			for _, publisher := range publishers {
				services = append(services, publisher.GetKind())
			}
			require.ElementsMatch(t, tt.expectedServices, services)
		})
	}
}
