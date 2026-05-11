package auths

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestProtectedEnvVarsWithoutAuthDependency(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext()
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}

	env, err := ProtectedEnvVars(ctx, stack, "payments", nil)
	require.NoError(t, err)
	require.Empty(t, env)
}

func TestProtectedEnvVarsWithAuthDependencyAndGateway(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		&v1beta1.Auth{
			TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Auth"},
			ObjectMeta: testutil.ObjectMeta("auth"),
			Spec:       v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
		},
		&v1beta1.Gateway{
			TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Gateway"},
			ObjectMeta: testutil.ObjectMeta("gateway"),
			Spec: v1beta1.GatewaySpec{
				StackDependency: v1beta1.StackDependency{Stack: "stack0"},
				Ingress: &v1beta1.GatewayIngress{
					Scheme: "https",
					Host:   "stack.example.com",
				},
			},
		},
		settings.New("issuers", "auth.issuers", "https://issuer-a https://issuer-b", "stack0"),
	)
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}

	env, err := ProtectedEnvVars(ctx, stack, "payments", &v1beta1.AuthConfig{
		ReadKeySetMaxRetries: 3,
		CheckScopes:          true,
	})
	require.NoError(t, err)

	values := testutil.EnvMap(env)
	require.Equal(t, "true", values["AUTH_ENABLED"])
	require.Equal(t, "https://stack.example.com/api/auth", values["AUTH_ISSUER"])
	require.Equal(t, "https://issuer-a https://issuer-b", values["AUTH_ISSUERS"])
	require.Equal(t, "3", values["AUTH_READ_KEY_SET_MAX_RETRIES"])
	require.Equal(t, "true", values["AUTH_CHECK_SCOPES"])
	require.Equal(t, "payments", values["AUTH_SERVICE"])
}

func TestProtectedEnvVarsUsesScopeSettingsWhenSpecDoesNotEnableScopes(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		&v1beta1.Auth{
			TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Auth"},
			ObjectMeta: testutil.ObjectMeta("auth"),
			Spec:       v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
		},
		settings.New("scope", "auth.payments.check-scopes", "true", "stack0"),
	)
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}

	env, err := ProtectedEnvVars(ctx, stack, "payments", nil)
	require.NoError(t, err)

	values := testutil.EnvMap(env)
	require.Equal(t, "http://auth:8080", values["AUTH_ISSUER"])
	require.Equal(t, "true", values["AUTH_CHECK_SCOPES"])
	require.Equal(t, "payments", values["AUTH_SERVICE"])
}
