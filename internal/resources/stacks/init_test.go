package stacks

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestSetModulesConditionMarksReadyModules(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", Generation: 7}}
	ctx := testutil.NewContext(moduleObject("Auth", "auth0", "stack0", metav1.ConditionTrue, "Spec", 7))

	require.NoError(t, setModulesCondition(ctx, stack))
	require.Equal(t, []string{"Auth"}, stack.Status.Modules)
	condition := stack.GetConditions().Get(ModuleReconciliation)
	require.NotNil(t, condition)
	require.Equal(t, "Auth", condition.Reason)
	require.Equal(t, metav1.ConditionTrue, condition.Status)
	require.Equal(t, "All checks passed", condition.Message)
	require.Equal(t, int64(7), condition.ObservedGeneration)
}

func TestSetModulesConditionDetectsPendingModule(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", Generation: 7}}
	ctx := testutil.NewContext(moduleObject("Auth", "auth0", "stack0", metav1.ConditionFalse, "Spec", 7))

	err := setModulesCondition(ctx, stack)
	require.True(t, core.IsApplicationError(err))
	require.Contains(t, err.Error(), "Pending modules")
	condition := stack.GetConditions().Get(ModuleReconciliation)
	require.NotNil(t, condition)
	require.Equal(t, "Auth", condition.Reason)
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	require.Equal(t, "Module not declared as reconciled for stack", condition.Message)
}

func TestSetModulesConditionDetectsSkipMismatch(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "stack0",
			Generation: 7,
			Annotations: map[string]string{
				v1beta1.SkipLabel: "true",
			},
		},
	}
	ctx := testutil.NewContext(moduleObject("Auth", "auth0", "stack0", metav1.ConditionTrue, "Spec", 7))

	err := setModulesCondition(ctx, stack)
	require.True(t, core.IsApplicationError(err))
	condition := stack.GetConditions().Get(ModuleReconciliation)
	require.NotNil(t, condition)
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	require.Equal(t, "Module should be skipped but is not", condition.Message)
}

func TestNamespaceMutators(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		settings.New("labels", "namespace.labels", "team=core,env=dev", "stack0"),
		settings.New("annotations", "namespace.annotations", "owner=platform", "stack0"),
	)
	ns := &corev1.Namespace{}

	require.NoError(t, namespaceLabel(ctx, "stack0")(ns))
	require.NoError(t, namespaceAnnotations(ctx, "stack0")(ns))
	require.Equal(t, map[string]string{"team": "core", "env": "dev"}, ns.Labels)
	require.Equal(t, map[string]string{"owner": "platform"}, ns.Annotations)
}

func moduleObject(kind, name, stack string, status metav1.ConditionStatus, reason string, observedGeneration int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "formance.com/v1beta1",
		"kind":       kind,
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"stack": stack,
		},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{
					"type":               "ReconciledWithStack",
					"status":             string(status),
					"reason":             reason,
					"observedGeneration": observedGeneration,
				},
			},
		},
	}}
}
