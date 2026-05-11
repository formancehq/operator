package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestGetOTELEnvVars(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		New("traces-dsn", "opentelemetry.traces.dsn", "grpc://otel-collector:4317?insecure=true", "stack0"),
		New("traces-attrs", "opentelemetry.traces.resource-attributes", "service.namespace=formance,region=eu", "stack0"),
		New("metrics-dsn", "opentelemetry.metrics.dsn", "http://otel-collector:4318/v1/metrics", "stack0"),
	)

	env, err := GetOTELEnvVars(ctx, "stack0", "payments", " ")
	require.NoError(t, err)
	envMap := testutil.EnvMap(env)

	require.Equal(t, "true", envMap["OTEL_TRACES"])
	require.Equal(t, "true", envMap["OTEL_TRACES_BATCH"])
	require.Equal(t, "otlp", envMap["OTEL_TRACES_EXPORTER"])
	require.Equal(t, "true", envMap["OTEL_TRACES_EXPORTER_OTLP_INSECURE"])
	require.Equal(t, "4317", envMap["OTEL_TRACES_PORT"])
	require.Equal(t, "otel-collector", envMap["OTEL_TRACES_ENDPOINT"])
	require.Equal(t, "$(OTEL_TRACES_ENDPOINT):$(OTEL_TRACES_PORT)", envMap["OTEL_TRACES_EXPORTER_OTLP_ENDPOINT"])
	require.Equal(t, "pod-name=$(POD_NAME) stack=stack0", envMap["OTEL_RESOURCE_ATTRIBUTES"])

	require.Equal(t, "true", envMap["OTEL_METRICS"])
	require.Equal(t, "true", envMap["OTEL_METRICS_RUNTIME"])
	require.Equal(t, "http://otel-collector:4318/v1/metrics", envMap["OTEL_METRICS_EXPORTER_OTLP_ENDPOINT"])
	require.Equal(t, "payments", envMap["OTEL_SERVICE_NAME"])

	var podName corev1.EnvVar
	for _, item := range env {
		if item.Name == "POD_NAME" {
			podName = item
			break
		}
	}
	require.Equal(t, "metadata.name", podName.ValueFrom.FieldRef.FieldPath)

	tracesEnv, err := otelEnvVars(ctx, "stack0", MonitoringTypeTraces, "payments", " ")
	require.NoError(t, err)
	require.Equal(t, "pod-name=$(POD_NAME) region=eu service.namespace=formance stack=stack0", testutil.EnvMap(tracesEnv)["OTEL_RESOURCE_ATTRIBUTES"])
}

func TestHasOpenTelemetryTracesEnabled(t *testing.T) {
	t.Parallel()

	enabled, err := HasOpenTelemetryTracesEnabled(testutil.NewContext(), "stack0")
	require.NoError(t, err)
	require.False(t, enabled)

	enabled, err = HasOpenTelemetryTracesEnabled(testutil.NewContext(
		New("traces-dsn", "opentelemetry.traces.dsn", "grpc://otel-collector:4317", "stack0"),
	), "stack0")
	require.NoError(t, err)
	require.True(t, enabled)
}

func TestGetResourceRequirements(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		New("limits", "jobs.payments.resources.limits", "cpu=500m,memory=256Mi", "stack0"),
		New("requests", "jobs.payments.resources.requests", "cpu=250m,memory=128Mi", "stack0"),
		New("claims", "jobs.payments.resources.claims", "cache,tmp", "stack0"),
	)

	requirements, err := GetResourceRequirements(ctx, "stack0", "jobs", "payments", "resources")
	require.NoError(t, err)
	require.Equal(t, "500m", requirements.Limits.Cpu().String())
	require.Equal(t, "256Mi", requirements.Limits.Memory().String())
	require.Equal(t, "250m", requirements.Requests.Cpu().String())
	require.Equal(t, "128Mi", requirements.Requests.Memory().String())
	require.Len(t, requirements.Claims, 2)
	require.Equal(t, "cache", requirements.Claims[0].Name)
	require.Equal(t, "tmp", requirements.Claims[1].Name)

	_, err = GetResourceList(testutil.NewContext(
		New("invalid", "jobs.payments.resources.limits", "cpu=not-a-quantity", "stack0"),
	), "stack0", "jobs", "payments", "resources", "limits")
	require.Error(t, err)
}
