package gateways

import (
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func createConfigMap(ctx core.Context, stack *v1beta1.Stack,
	gateway *v1beta1.Gateway, httpAPIs []*v1beta1.GatewayHTTPAPI, broker *v1beta1.Broker) (*v1.ConfigMap, error) {

	options := []CaddyOptions{}

	trustedProxies, err := settings.GetStringSlice(ctx, stack.Name, "gateway", "caddyfile", "trusted-proxies")
	if err != nil {
		return nil, err
	}
	if trustedProxies != nil {
		options = append(options, withTrustedProxies(trustedProxies))
	}

	trustedProxiesStrict, err := settings.GetBool(ctx, stack.Name, "gateway", "caddyfile", "trusted-proxies-strict")
	if err != nil {
		return nil, err
	}
	if trustedProxiesStrict != nil && *trustedProxiesStrict {
		options = append(options, withTrustedProxiesStrict())
	}

	caddyfile, err := CreateCaddyfile(ctx, stack, gateway, httpAPIs, broker, options...)
	if err != nil {
		return nil, err
	}

	caddyfileConfigMap, _, err := core.CreateOrUpdate[*v1.ConfigMap](ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      "gateway",
	},
		func(t *v1.ConfigMap) error {
			t.Data = map[string]string{
				"Caddyfile": caddyfile,
			}

			return nil
		},
		core.WithController[*v1.ConfigMap](ctx.GetScheme(), gateway),
	)

	return caddyfileConfigMap, err
}
