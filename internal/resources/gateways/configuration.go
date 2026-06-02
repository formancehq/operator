package gateways

import (
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func createConfigMap(ctx core.Context, stack *v1beta1.Stack,
	gateway *v1beta1.Gateway, httpAPIs []*v1beta1.GatewayHTTPAPI, broker *v1beta1.Broker) (*v1.ConfigMap, error) {

	options := []CaddyOptions{}

	caddyfileSpec := gateway.Spec.Caddyfile

	var trustedProxiesSpec []string
	if caddyfileSpec != nil {
		trustedProxiesSpec = caddyfileSpec.TrustedProxies
	}
	trustedProxies, err := settings.PreferSpecStringSlice(ctx, stack.Name, trustedProxiesSpec,
		"Gateway.Spec.Caddyfile.TrustedProxies", "gateway", "caddyfile", "trusted-proxies")
	if err != nil {
		return nil, err
	}
	if trustedProxies != nil {
		options = append(options, withTrustedProxies(trustedProxies))
	}

	var trustedProxiesStrictSpec *bool
	if caddyfileSpec != nil {
		trustedProxiesStrictSpec = caddyfileSpec.TrustedProxiesStrict
	}
	trustedProxiesStrict, err := settings.PreferSpecBool(ctx, stack.Name, trustedProxiesStrictSpec,
		"Gateway.Spec.Caddyfile.TrustedProxiesStrict", "gateway", "caddyfile", "trusted-proxies-strict")
	if err != nil {
		return nil, err
	}
	if trustedProxiesStrict {
		options = append(options, withTrustedProxiesStrict())
	}

	var shutdownDelaySpec time.Duration
	if caddyfileSpec != nil && caddyfileSpec.ShutdownDelay != nil {
		shutdownDelaySpec = caddyfileSpec.ShutdownDelay.Duration
	}
	shutdownDelay, err := settings.PreferSpecDuration(ctx, stack.Name, shutdownDelaySpec, 0,
		"Gateway.Spec.Caddyfile.ShutdownDelay", "gateway", "caddyfile", "shutdown-delay")
	if err != nil {
		return nil, err
	}
	if shutdownDelay != 0 {
		options = append(options, withShutdownDelay(shutdownDelay))
	}

	var gracePeriodSpec time.Duration
	if caddyfileSpec != nil && caddyfileSpec.GracePeriod != nil {
		gracePeriodSpec = caddyfileSpec.GracePeriod.Duration
	}
	gracePeriod, err := settings.PreferSpecDuration(ctx, stack.Name, gracePeriodSpec, 0,
		"Gateway.Spec.Caddyfile.GracePeriod", "gateway", "caddyfile", "grace-period")
	if err != nil {
		return nil, err
	}
	if gracePeriod != 0 {
		options = append(options, withGracePeriod(gracePeriod))
	}

	var idleTimeoutSpec time.Duration
	if gateway.Spec.Config != nil && gateway.Spec.Config.IdleTimeout != nil {
		idleTimeoutSpec = gateway.Spec.Config.IdleTimeout.Duration
	}
	idleTimeout, err := settings.PreferSpecDuration(ctx, stack.Name, idleTimeoutSpec, 0,
		"Gateway.Spec.Config.IdleTimeout", "gateway", "config", "idle-timeout")
	if err != nil {
		return nil, err
	}
	if idleTimeout != 0 {
		options = append(options, withIdleTimeout(idleTimeout))
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
