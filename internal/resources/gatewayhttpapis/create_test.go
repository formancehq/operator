package gatewayhttpapis

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestRuleHelpers(t *testing.T) {
	t.Parallel()

	require.False(t, RuleSecured().Secured)
	require.True(t, RuleUnsecured().Secured)

	api := &v1beta1.GatewayHTTPAPI{}
	WithRules(v1beta1.GatewayHTTPAPIRule{Path: "/api", Secured: true})(api)
	WithHealthCheckEndpoint("_health")(api)
	require.Equal(t, []v1beta1.GatewayHTTPAPIRule{{Path: "/api", Secured: true}}, api.Spec.Rules)
	require.Equal(t, "_health", api.Spec.HealthCheckEndpoint)
}

func TestCreateGatewayHTTPAPI(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext()
	owner := &v1beta1.Payments{
		TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Payments"},
		ObjectMeta: metav1.ObjectMeta{Name: "payments", UID: "uid-123"},
		Spec:       v1beta1.PaymentsSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
	}

	err := Create(ctx, owner,
		WithHealthCheckEndpoint("_healthcheck"),
		WithRules(v1beta1.GatewayHTTPAPIRule{Path: "/payments", Methods: []string{"GET"}, Secured: true}),
	)
	require.NoError(t, err)

	api := &v1beta1.GatewayHTTPAPI{}
	require.NoError(t, ctx.GetClient().Get(context.Background(), types.NamespacedName{Name: "stack0-payments"}, api))
	require.Equal(t, "stack0", api.Spec.Stack)
	require.Equal(t, "payments", api.Spec.Name)
	require.Equal(t, "_healthcheck", api.Spec.HealthCheckEndpoint)
	require.Equal(t, []v1beta1.GatewayHTTPAPIRule{{Path: "/payments", Methods: []string{"GET"}, Secured: true}}, api.Spec.Rules)
	require.Len(t, api.OwnerReferences, 1)
	require.Equal(t, "Payments", api.OwnerReferences[0].Kind)
}
