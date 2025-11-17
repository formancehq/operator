package gateways

import (
	"strings"

	"github.com/formancehq/go-libs/v2/collectionutils"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/caddy"
)

type CaddyOptions func(data map[string]any) error

func CreateCaddyfile(ctx core.Context, stack *v1beta1.Stack,
	gateway *v1beta1.Gateway, httpAPIs []*v1beta1.GatewayHTTPAPI, broker *v1beta1.Broker, options ...CaddyOptions) (string, error) {

	data := map[string]any{
		"Services": collectionutils.Map(httpAPIs, func(from *v1beta1.GatewayHTTPAPI) v1beta1.GatewayHTTPAPISpec {
			return from.Spec
		}),
		"Platform": ctx.GetPlatform(),
		"Debug":    stack.Spec.Debug,
		"Port":     8080,
		"Gateway": map[string]any{
			"Version": gateway.Spec.Version,
		},
	}

	if broker != nil {
		data["EnableAudit"] = true
		data["Broker"] = broker.Status.URI.Scheme
	}

	for _, option := range options {
		if err := option(data); err != nil {
			return "", err
		}
	}

	return caddy.ComputeCaddyfile(ctx, stack, Caddyfile, data)
}

func withTrustedProxies(options []string) func(data map[string]any) error {
	return func(data map[string]any) error {
		data["TrustedProxies"] = strings.Join(options, " ")
		return nil
	}
}

func withTrustedProxiesStrict() func(data map[string]any) error {
	return func(data map[string]any) error {
		data["TrustedProxiesStrict"] = true
		return nil
	}
}
