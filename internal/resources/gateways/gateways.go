package gateways

import (
	_ "embed"
	"fmt"

	v1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

//go:embed Caddyfile.gotpl
var Caddyfile string

// InClusterAddress is the host:port of the stack Gateway Service as seen from
// pods in the stack namespace. The listener serves HTTP/1.1 and h2c, so gRPC
// clients dial it in plaintext.
const InClusterAddress = "gateway:8080"

func EnvVarsIfEnabled(ctx core.Context, stackName string) ([]v1.EnvVar, error) {
	gateway := &v1beta1.Gateway{}
	ok, err := core.GetIfExists(ctx, stackName, gateway)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	return GetEnvVars(gateway), nil
}

func GetEnvVars(gateway *v1beta1.Gateway) []v1.EnvVar {
	ret := []v1.EnvVar{{
		Name:  "STACK_URL",
		Value: "http://gateway:8080",
	}}
	if gateway.Spec.Ingress != nil {
		ret = append(ret, v1.EnvVar{
			Name:  "STACK_PUBLIC_URL",
			Value: fmt.Sprintf("%s://%s", gateway.Spec.Ingress.Scheme, gateway.Spec.Ingress.Host),
		})
	}

	return ret
}
