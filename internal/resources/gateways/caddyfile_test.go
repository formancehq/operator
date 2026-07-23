package gateways

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestCaddyHTTPServicesUsesRootRuleBackendForHealthChecks(t *testing.T) {
	t.Parallel()

	services := caddyHTTPServices([]*v1beta1.GatewayHTTPAPI{{
		Spec: v1beta1.GatewayHTTPAPISpec{
			Name: "ledger",
			Rules: []v1beta1.GatewayHTTPAPIRule{
				{Path: "/v3", BackendRef: &v1beta1.GatewayBackendRef{Name: "ledger-preview", Port: 9000}},
				{BackendRef: &v1beta1.GatewayBackendRef{Name: "ledger-stack0", Port: 9000}},
			},
		},
	}})

	require.Len(t, services, 1)
	require.Equal(t, v1beta1.GatewayBackendRef{Name: "ledger-stack0", Port: 9000}, services[0].HealthCheckBackend)
	require.Equal(t, "/v3", services[0].Rules[0].Path, "route specificity must still be preserved")
}

func TestCaddyHTTPServicesKeepsHistoricalHealthCheckBackend(t *testing.T) {
	t.Parallel()

	services := caddyHTTPServices([]*v1beta1.GatewayHTTPAPI{{
		Spec: v1beta1.GatewayHTTPAPISpec{
			Name:  "ledger",
			Rules: []v1beta1.GatewayHTTPAPIRule{{}},
		},
	}})

	require.Len(t, services, 1)
	require.Equal(t, v1beta1.GatewayBackendRef{Name: "ledger", Port: 8080}, services[0].HealthCheckBackend)
}
