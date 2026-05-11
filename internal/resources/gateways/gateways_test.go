package gateways

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestGetEnvVarsAndURL(t *testing.T) {
	t.Parallel()

	gateway := &v1beta1.Gateway{}
	require.Equal(t, "http://gateway:8080", URL(gateway))
	require.Equal(t, map[string]string{"STACK_URL": "http://gateway:8080"}, testutil.EnvMap(GetEnvVars(gateway)))

	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack.example.com"}
	require.Equal(t, "https://stack.example.com", URL(gateway))
	require.Equal(t, map[string]string{
		"STACK_URL":        "http://gateway:8080",
		"STACK_PUBLIC_URL": "https://stack.example.com",
	}, testutil.EnvMap(GetEnvVars(gateway)))
}

func TestEnvVarsIfEnabled(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(&v1beta1.Gateway{
		TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Gateway"},
		ObjectMeta: testutil.ObjectMeta("gateway"),
		Spec: v1beta1.GatewaySpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Ingress:         &v1beta1.GatewayIngress{Scheme: "https", Host: "stack.example.com"},
		},
	})

	env, err := EnvVarsIfEnabled(ctx, "stack0")
	require.NoError(t, err)
	require.Equal(t, "https://stack.example.com", testutil.EnvMap(env)["STACK_PUBLIC_URL"])

	env, err = EnvVarsIfEnabled(ctx, "missing")
	require.NoError(t, err)
	require.Nil(t, env)
}
