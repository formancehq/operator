package databases

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestGetPostgresEnvVarsWithPasswordAndConnectionPool(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		settings.New("aws", "aws.service-account", "aws-access", "stack0"),
		settings.New("pool", "modules.ledger.database.connection-pool", "max-idle=10,max-idle-time=30s,max-open=20,max-lifetime=1h", "stack0"),
	)
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	database := &v1beta1.Database{
		Spec: v1beta1.DatabaseSpec{Service: "ledger"},
		Status: v1beta1.DatabaseStatus{
			Database: "ledger-db",
			URI:      testutil.MustParseURI("postgresql://ledger:p%40ss%20word@postgres:5432?disableSSLMode=true&application_name=operator&awsRole=ignored"),
		},
	}

	env, err := GetPostgresEnvVars(ctx, stack, database)
	require.NoError(t, err)

	values := testutil.EnvMap(env)
	require.Equal(t, "postgres", values["POSTGRES_HOST"])
	require.Equal(t, "5432", values["POSTGRES_PORT"])
	require.Equal(t, "ledger-db", values["POSTGRES_DATABASE"])
	require.Equal(t, "ledger", values["POSTGRES_USERNAME"])
	require.Equal(t, "p%40ss+word", values["POSTGRES_PASSWORD"])
	require.Equal(t, "true", values["POSTGRES_AWS_ENABLE_IAM"])
	require.Equal(t, "$(POSTGRES_NO_DATABASE_URI)/$(POSTGRES_DATABASE)?application_name=operator&sslmode=disable", values["POSTGRES_URI"])
	require.Equal(t, "10", values["POSTGRES_MAX_IDLE_CONNS"])
	require.Equal(t, "30s", values["POSTGRES_CONN_MAX_IDLE_TIME"])
	require.Equal(t, "20", values["POSTGRES_MAX_OPEN_CONNS"])
	require.Equal(t, "1h", values["POSTGRES_CONN_MAX_LIFETIME"])
}

func TestGetPostgresEnvVarsWithSecretCredentials(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext()
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	database := &v1beta1.Database{
		Spec: v1beta1.DatabaseSpec{Service: "payments"},
		Status: v1beta1.DatabaseStatus{
			Database: "payments-db",
			URI:      testutil.MustParseURI("postgresql://postgres:5432?secret=pg-creds&sslmode=require"),
		},
	}

	env, err := GetPostgresEnvVars(ctx, stack, database)
	require.NoError(t, err)

	values := testutil.EnvMap(env)
	require.Equal(t, "postgres", values["POSTGRES_HOST"])
	require.Equal(t, "5432", values["POSTGRES_PORT"])
	require.Equal(t, "$(POSTGRES_NO_DATABASE_URI)/$(POSTGRES_DATABASE)?sslmode=require", values["POSTGRES_URI"])

	requireSecretEnv(t, env, "POSTGRES_USERNAME", "pg-creds", "username")
	requireSecretEnv(t, env, "POSTGRES_PASSWORD", "pg-creds", "password")
}

func TestGetPostgresEnvVarsRejectsInvalidConnectionPool(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "max idle", value: "max-idle=invalid"},
		{name: "max idle time", value: "max-idle-time=invalid"},
		{name: "max open", value: "max-open=invalid"},
		{name: "max lifetime", value: "max-lifetime=invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := testutil.NewContext(
				settings.New("pool", "modules.ledger.database.connection-pool", tc.value, "stack0"),
			)
			stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
			database := &v1beta1.Database{
				Spec: v1beta1.DatabaseSpec{Service: "ledger"},
				Status: v1beta1.DatabaseStatus{
					Database: "ledger-db",
					URI:      testutil.MustParseURI("postgresql://postgres:5432"),
				},
			}

			_, err := GetPostgresEnvVars(ctx, stack, database)
			require.Error(t, err)
		})
	}
}

func requireSecretEnv(t *testing.T, env []corev1.EnvVar, name, secret, key string) {
	t.Helper()

	for _, item := range env {
		if item.Name != name {
			continue
		}
		require.NotNil(t, item.ValueFrom)
		require.NotNil(t, item.ValueFrom.SecretKeyRef)
		require.Equal(t, secret, item.ValueFrom.SecretKeyRef.Name)
		require.Equal(t, key, item.ValueFrom.SecretKeyRef.Key)
		return
	}
	require.Failf(t, "missing env var", "env var %s was not found", name)
}
