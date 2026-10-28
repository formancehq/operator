package ledgers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

var incompatibleModuleFactories = []struct {
	name      string
	newModule func() v1beta1.Module
}{
	{"MCP", func() v1beta1.Module { return &v1beta1.MCP{} }},
	{"Orchestration", func() v1beta1.Module { return &v1beta1.Orchestration{} }},
	{"Payments", func() v1beta1.Module { return &v1beta1.Payments{} }},
	{"Reconciliation", func() v1beta1.Module { return &v1beta1.Reconciliation{} }},
	{"TransactionPlane", func() v1beta1.Module { return &v1beta1.TransactionPlane{} }},
	{"Wallets", func() v1beta1.Module { return &v1beta1.Wallets{} }},
	{"Webhooks", func() v1beta1.Module { return &v1beta1.Webhooks{} }},
}

type disabledCompatibilityContext struct {
	context.Context
	client client.Client
	scheme *runtime.Scheme
}

func (c disabledCompatibilityContext) GetClient() client.Client    { return c.client }
func (c disabledCompatibilityContext) GetAPIReader() client.Reader { return c.client }
func (c disabledCompatibilityContext) GetScheme() *runtime.Scheme  { return c.scheme }
func (c disabledCompatibilityContext) GetPlatform() core.Platform  { return core.Platform{} }

func newDisabledCompatibilityContext(t *testing.T, objects ...client.Object) disabledCompatibilityContext {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	scheme.AddKnownTypeWithName(ledgerV3ClusterGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(ledgerV3ClusterGVK.GroupVersion().WithKind("ClusterList"), &unstructured.UnstructuredList{})
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...)
	for _, candidate := range incompatibleModuleFactories {
		builder = builder.WithIndex(candidate.newModule(), "stack", func(object client.Object) []string {
			return []string{object.(v1beta1.Module).GetStack()}
		})
	}
	return disabledCompatibilityContext{Context: context.Background(), client: builder.Build(), scheme: scheme}
}

func incompatibleModule(t *testing.T, factory func() v1beta1.Module, name string, disabled bool) v1beta1.Module {
	t.Helper()
	module := factory()
	spec, err := json.Marshal(map[string]any{"spec": map[string]any{"stack": "stack", "disabled": disabled}})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(spec, module))
	module.SetName(name)
	return module
}

func TestV3TransitionDisabledCompatibility(t *testing.T) {
	for _, candidate := range incompatibleModuleFactories {
		t.Run(candidate.name, func(t *testing.T) {
			for _, scenario := range []string{"enabled", "disabled", "mixed", "all-disabled", "deleting", "other-stack"} {
				t.Run(scenario, func(t *testing.T) {
					module := incompatibleModule(t, candidate.newModule, "module", scenario != "enabled")
					objects := []client.Object{module}
					switch scenario {
					case "mixed":
						objects = append(objects, incompatibleModule(t, candidate.newModule, "enabled", false))
					case "all-disabled":
						objects = append(objects, incompatibleModule(t, candidate.newModule, "also-disabled", true))
					case "deleting":
						module = incompatibleModule(t, candidate.newModule, "module", false)
						now := metav1.Now()
						module.SetDeletionTimestamp(&now)
						module.SetFinalizers([]string{"cleanup"})
						objects = []client.Object{module}
					case "other-stack":
						require.NoError(t, json.Unmarshal([]byte(`{"spec":{"stack":"other","disabled":false}}`), module))
					}
					ctx := newDisabledCompatibilityContext(t, objects...)
					stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack"}}
					ledger := &v1beta1.Ledger{}
					err := blockV3TransitionWithIncompatibleModules(ctx, stack, ledger)
					if scenario == "enabled" || scenario == "mixed" {
						require.True(t, core.IsApplicationError(err), "%v", err)
						condition := ledger.GetConditions().Get(ledgerV3ClusterReadyCondition)
						require.NotNil(t, condition)
						require.Equal(t, "IncompatibleModules", condition.Reason)
						require.Contains(t, condition.Message, candidate.name)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}

func TestV3TransitionMixedDisabledKinds(t *testing.T) {
	var objects []client.Object
	for _, candidate := range incompatibleModuleFactories {
		objects = append(objects, incompatibleModule(t, candidate.newModule, "module", candidate.name != "Wallets"))
	}
	ctx := newDisabledCompatibilityContext(t, objects...)
	ledger := &v1beta1.Ledger{}
	err := blockV3TransitionWithIncompatibleModules(ctx, &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack"}}, ledger)
	require.True(t, core.IsApplicationError(err), "%v", err)
	condition := ledger.GetConditions().Get(ledgerV3ClusterReadyCondition)
	require.NotNil(t, condition)
	require.Equal(t, "disable incompatible modules before switching Ledger to v3: Wallets", condition.Message)
}

func TestV3TransitionDisabledLookupFailure(t *testing.T) {
	ctx := newDisabledCompatibilityContext(t)
	lookupErr := errors.New("dependency list unavailable")
	ctx.client = interceptor.NewClient(ctx.client.(client.WithWatch), interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return lookupErr
		},
	})
	err := blockV3TransitionWithIncompatibleModules(ctx, &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack"}}, &v1beta1.Ledger{})
	require.ErrorIs(t, err, lookupErr)
}
