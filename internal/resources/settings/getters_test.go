package settings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/operator/v3/internal/testutil"
)

type typedSetting struct {
	MaxIdle     string `json:"max-idle,omitempty"`
	MaxLifetime string `json:"max-lifetime,omitempty"`
}

func TestGetTypedSettingsFromFakeClient(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		New("wildcard", "modules.*.database.connection-pool", "max-idle=5,max-lifetime=30m", "*"),
		New("stack-specific", "modules.ledger.database.connection-pool", "max-idle=10,max-lifetime=1h", "stack0"),
		New("slice", "ledger.experimental-numscript-flags", " a, b ,,c ", "stack0"),
		New("env", "jobs.payments.containers.migrate.env-vars", `FOO=bar,QUOTED="a,b"`, "stack0"),
		New("bool", "auth.payments.check-scopes", "true", "stack0"),
		New("int", "payments.worker.temporal-max-slots-per-poller", "12", "stack0"),
	)

	value, err := GetStringOrEmpty(ctx, "stack0", "modules", "ledger", "database", "connection-pool")
	require.NoError(t, err)
	require.Equal(t, "max-idle=10,max-lifetime=1h", value)

	value, err = GetStringOrEmpty(ctx, "stack1", "modules", "payments", "database", "connection-pool")
	require.NoError(t, err)
	require.Equal(t, "max-idle=5,max-lifetime=30m", value)

	flags, err := GetTrimmedStringSlice(ctx, "stack0", "ledger", "experimental-numscript-flags")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c"}, flags)

	env, err := GetEnvVars(ctx, "stack0", "jobs", "payments", "containers", "migrate")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"FOO": "bar", "QUOTED": "a,b"}, testutil.EnvMap(env))

	checkScopes, err := GetBoolOrFalse(ctx, "stack0", "auth", "payments", "check-scopes")
	require.NoError(t, err)
	require.True(t, checkScopes)

	slots, err := GetIntOrDefault(ctx, "stack0", 4, "payments", "worker", "temporal-max-slots-per-poller")
	require.NoError(t, err)
	require.Equal(t, 12, slots)

	cfg, err := GetAs[typedSetting](ctx, "stack0", "modules", "ledger", "database", "connection-pool")
	require.NoError(t, err)
	require.Equal(t, &typedSetting{MaxIdle: "10", MaxLifetime: "1h"}, cfg)
}

func TestSettingsErrors(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		New("invalid-int", "payments.worker.count", "not-an-int", "stack0"),
		New("invalid-map", "jobs.payments.env-vars", `BROKEN="unterminated`, "stack0"),
	)

	_, err := RequireString(ctx, "stack0", "missing", "setting")
	require.Error(t, err)

	_, err = GetInt(ctx, "stack0", "payments", "worker", "count")
	require.Error(t, err)

	_, err = GetMap(ctx, "stack0", "jobs", "payments", "env-vars")
	require.Error(t, err)
}

func TestNumericURLAndBoolGetters(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		New("int64", "numbers.int64", "922337203685477580", "stack0"),
		New("int32", "numbers.int32", "214748364", "stack0"),
		New("uint64", "numbers.uint64", "1844674407370955161", "stack0"),
		New("uint16", "numbers.uint16", "65535", "stack0"),
		New("uint", "numbers.uint", "42", "stack0"),
		New("bool-true", "flags.enabled", "true", "stack0"),
		New("bool-false", "flags.disabled", "false", "stack0"),
		New("url", "temporal.dsn", "temporal://temporal.stack0:7233/payments", "stack0"),
	)

	int64Value, err := GetInt64(ctx, "stack0", "numbers", "int64")
	require.NoError(t, err)
	require.Equal(t, int64(922337203685477580), *int64Value)

	int32Value, err := GetInt32(ctx, "stack0", "numbers", "int32")
	require.NoError(t, err)
	require.Equal(t, int32(214748364), *int32Value)

	uint64Value, err := GetUInt64(ctx, "stack0", "numbers", "uint64")
	require.NoError(t, err)
	require.Equal(t, uint64(1844674407370955161), *uint64Value)

	uint16Value, err := GetUInt16(ctx, "stack0", "numbers", "uint16")
	require.NoError(t, err)
	require.Equal(t, uint16(65535), *uint16Value)

	uintValue, err := GetUInt(ctx, "stack0", "numbers", "uint")
	require.NoError(t, err)
	require.Equal(t, uint(42), *uintValue)

	uint16Default, err := GetUInt16OrDefault(ctx, "stack0", 7, "missing", "uint16")
	require.NoError(t, err)
	require.Equal(t, uint16(7), uint16Default)

	int32Default, err := GetInt32OrDefault(ctx, "stack0", 9, "missing", "int32")
	require.NoError(t, err)
	require.Equal(t, int32(9), int32Default)

	enabled, err := GetBool(ctx, "stack0", "flags", "enabled")
	require.NoError(t, err)
	require.True(t, *enabled)

	disabled, err := GetBool(ctx, "stack0", "flags", "disabled")
	require.NoError(t, err)
	require.False(t, *disabled)

	enabledDefault, err := GetBoolOrDefault(ctx, "stack0", true, "missing", "flag")
	require.NoError(t, err)
	require.True(t, enabledDefault)

	uri, err := GetURL(ctx, "stack0", "temporal", "dsn")
	require.NoError(t, err)
	require.Equal(t, "temporal", uri.Scheme)
	require.Equal(t, "temporal.stack0:7233", uri.Host)
	require.Equal(t, "/payments", uri.Path)

	requiredURI, err := RequireURL(ctx, "stack0", "temporal", "dsn")
	require.NoError(t, err)
	require.Equal(t, uri.String(), requiredURI.String())
}

func TestGetterParsingErrors(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		New("invalid-int32", "numbers.int32", "999999999999999999999", "stack0"),
		New("invalid-uint", "numbers.uint", "-1", "stack0"),
		New("invalid-url", "temporal.dsn", "%", "stack0"),
	)

	_, err := GetInt32(ctx, "stack0", "numbers", "int32")
	require.Error(t, err)

	_, err = GetUInt(ctx, "stack0", "numbers", "uint")
	require.Error(t, err)

	_, err = GetURL(ctx, "stack0", "temporal", "dsn")
	require.Error(t, err)

	_, err = RequireURL(ctx, "stack0", "missing", "dsn")
	require.Error(t, err)
}
