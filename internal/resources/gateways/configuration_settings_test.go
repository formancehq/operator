package gateways

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestCreateConfigMapAppliesCaddyfileSettings(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	gateway := gatewaySettingsFixture()
	httpAPI := &v1beta1.GatewayHTTPAPI{
		Spec: v1beta1.GatewayHTTPAPISpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Name:            "ledger",
			Rules: []v1beta1.GatewayHTTPAPIRule{{
				Path:    "/accounts",
				Methods: []string{"GET", "POST"},
			}},
			HealthCheckEndpoint: "_health",
		},
	}
	ctx := testutil.NewContext(
		stack,
		gateway,
		settingspkg.New("trusted-proxies", "gateway.caddyfile.trusted-proxies", "10.0.0.0/8,192.168.0.0/16", "stack0"),
		settingspkg.New("trusted-proxies-strict", "gateway.caddyfile.trusted-proxies-strict", "true", "stack0"),
		settingspkg.New("idle-timeout", "gateway.config.idle-timeout", "30s", "stack0"),
	)

	configMap, err := createConfigMap(ctx, stack, gateway, []*v1beta1.GatewayHTTPAPI{httpAPI}, nil)
	require.NoError(t, err)

	stored := &corev1.ConfigMap{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "gateway", Namespace: "stack0"}, stored))
	require.Equal(t, configMap.Data["Caddyfile"], stored.Data["Caddyfile"])
	caddyfile := stored.Data["Caddyfile"]
	require.Contains(t, caddyfile, "trusted_proxies 10.0.0.0/8 192.168.0.0/16")
	require.Contains(t, caddyfile, "trusted_proxies_strict")
	require.Contains(t, caddyfile, "timeouts")
	require.Contains(t, caddyfile, "idle 30s")
	require.Contains(t, caddyfile, "handle /api/ledger/accounts*")
	require.Contains(t, caddyfile, "method GET POST")
	require.Len(t, stored.OwnerReferences, 1)
	require.Equal(t, "Gateway", stored.OwnerReferences[0].Kind)
}
