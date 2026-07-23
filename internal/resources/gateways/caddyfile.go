package gateways

import (
	"sort"
	"strings"
	"time"

	collectionutils "github.com/formancehq/go-libs/v5/pkg/types/collections"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/caddy"
)

type CaddyOptions func(data map[string]any) error

type caddyHTTPService struct {
	v1beta1.GatewayHTTPAPISpec
	HealthCheckBackend v1beta1.GatewayBackendRef
}

func CreateCaddyfile(ctx core.Context, stack *v1beta1.Stack,
	gateway *v1beta1.Gateway, httpAPIs []*v1beta1.GatewayHTTPAPI,
	grpcAPIs []*v1beta1.GatewayGRPCAPI, broker *v1beta1.Broker, options ...CaddyOptions) (string, error) {

	services := caddyHTTPServices(httpAPIs)

	data := map[string]any{
		"Services": services,
		"GRPCServices": collectionutils.Map(grpcAPIs, func(from *v1beta1.GatewayGRPCAPI) v1beta1.GatewayGRPCAPISpec {
			spec := *from.Spec.DeepCopy()
			normalizeBackendTLS(spec.BackendRef)
			return spec
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

func caddyHTTPServices(httpAPIs []*v1beta1.GatewayHTTPAPI) []caddyHTTPService {
	return collectionutils.Map(httpAPIs, func(from *v1beta1.GatewayHTTPAPI) caddyHTTPService {
		spec := *from.Spec.DeepCopy()
		healthCheckBackend := v1beta1.GatewayBackendRef{Name: spec.Name, Port: 8080}
		for i := range spec.Rules {
			normalizeBackendTLS(spec.Rules[i].BackendRef)
			if spec.Rules[i].Path == "" && spec.Rules[i].BackendRef != nil {
				healthCheckBackend = *spec.Rules[i].BackendRef.DeepCopy()
			}
		}
		sort.SliceStable(spec.Rules, func(i, j int) bool {
			return len(spec.Rules[i].Path) > len(spec.Rules[j].Path)
		})
		return caddyHTTPService{
			GatewayHTTPAPISpec: spec,
			HealthCheckBackend: healthCheckBackend,
		}
	})
}

func normalizeBackendTLS(backendRef *v1beta1.GatewayBackendRef) {
	if backendRef != nil && backendRef.TLS != nil && backendRef.TLS.CASecretKey == "" {
		backendRef.TLS.CASecretKey = "ca.crt"
	}
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

func withIdleTimeout(timeout time.Duration) func(data map[string]any) error {
	return func(data map[string]any) error {
		data["IdleTimeout"] = timeout.String()
		return nil
	}
}

func withShutdownDelay(delay time.Duration) func(data map[string]any) error {
	return func(data map[string]any) error {
		data["ShutdownDelay"] = delay.String()
		return nil
	}
}

func withGracePeriod(period time.Duration) func(data map[string]any) error {
	return func(data map[string]any) error {
		data["GracePeriod"] = period.String()
		return nil
	}
}
