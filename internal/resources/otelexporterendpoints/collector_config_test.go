package otelexporterendpoints

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func endpoint(name string, spec v1beta1.OtelExporterEndpointSpec) *v1beta1.OtelExporterEndpoint {
	return &v1beta1.OtelExporterEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       spec,
	}
}

func TestGenerateMergedCollectorConfig(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name                string
		inputs              []collectorInput
		otelSettings        *otelSettingsInput
		expectedContains    []string
		expectedNotContains []string
	}

	testCases := []testCase{
		{
			name: "single CRD with traces endpoint",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("monitoring", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "http://my-collector:4318",
						},
					}),
				},
			},
			expectedContains: []string{
				"otlphttp/monitoring-traces",
				"http://my-collector:4318",
				"nop",
				"health_check",
				"13133",
			},
		},
		{
			name: "single CRD with grpc endpoint",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("monitoring", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "grpc://my-collector:4317",
						},
					}),
				},
			},
			expectedContains: []string{
				"otlp/monitoring-traces",
				"my-collector:4317",
			},
			expectedNotContains: []string{"otlphttp/monitoring-traces"},
		},
		{
			name: "single CRD with auth",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("support", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "https://support.frmnc.net",
							Auth: &v1beta1.OtelExporterAuth{
								Type:       "bearer",
								FromSecret: "formance-license",
							},
						},
					}),
					TracesEnvAlias: "AUTH_SUPPORT_TRACES",
				},
			},
			expectedContains: []string{
				"otlphttp/support-traces",
				"https://support.frmnc.net",
				"authorization: Bearer ${env:AUTH_SUPPORT_TRACES}",
			},
		},
		{
			name: "multiple CRDs fan out",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("monitoring", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "http://my-collector:4318",
						},
					}),
				},
				{
					Endpoint: endpoint("support", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "https://support.frmnc.net",
							Auth: &v1beta1.OtelExporterAuth{
								Type:       "bearer",
								FromSecret: "formance-license",
							},
						},
					}),
					TracesEnvAlias: "AUTH_SUPPORT_TRACES",
				},
			},
			expectedContains: []string{
				"otlphttp/monitoring-traces",
				"http://my-collector:4318",
				"otlphttp/support-traces",
				"https://support.frmnc.net",
				"authorization: Bearer ${env:AUTH_SUPPORT_TRACES}",
			},
		},
		{
			name: "grpc endpoint with insecure query param",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("monitoring", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "grpc://my-collector:4317?insecure=true",
						},
					}),
				},
			},
			expectedContains: []string{
				"otlp/monitoring-traces",
				"my-collector:4317",
				"tls:",
				"insecure: true",
			},
			expectedNotContains: []string{"otlphttp/monitoring-traces"},
		},
		{
			name: "grpc endpoint without insecure has no tls config",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("monitoring", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "grpc://my-collector:4317",
						},
					}),
				},
			},
			expectedContains:    []string{"otlp/monitoring-traces", "my-collector:4317"},
			expectedNotContains: []string{"tls:"},
		},
		{
			name:   "otel settings grpc with insecure",
			inputs: []collectorInput{},
			otelSettings: &otelSettingsInput{
				TracesEndpoint:  "grpc://settings-collector:4317?insecure=true",
				MetricsEndpoint: "grpc://settings-collector:4317?insecure=true",
			},
			expectedContains: []string{
				"otlp/settings-traces",
				"otlp/settings-metrics",
				"settings-collector:4317",
				"tls:",
				"insecure: true",
			},
		},
		{
			name:                "no endpoints produces nop",
			inputs:              []collectorInput{},
			expectedContains:    []string{"nop"},
			expectedNotContains: []string{"otlphttp", "otlp/"},
		},
		{
			name:   "otel settings traces",
			inputs: []collectorInput{},
			otelSettings: &otelSettingsInput{
				TracesEndpoint: "http://settings-collector:4318",
			},
			expectedContains: []string{
				"otlphttp/settings-traces",
				"http://settings-collector:4318",
			},
		},
		{
			name: "CRD plus otel settings both appear",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("monitoring", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "http://my-collector:4318",
						},
					}),
				},
			},
			otelSettings: &otelSettingsInput{
				TracesEndpoint: "http://settings-collector:4318",
			},
			expectedContains: []string{
				"otlphttp/monitoring-traces",
				"otlphttp/settings-traces",
			},
		},
		{
			name: "resource attributes produce processor",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("support", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "https://support.frmnc.net",
						},
						ResourceAttributes: map[string]string{
							"cluster.id": "abc-123",
						},
					}),
				},
			},
			expectedContains: []string{
				"resource/support",
				"cluster.id",
				"abc-123",
				"upsert",
			},
		},
		{
			name: "traces and metrics with separate endpoints",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("monitoring", v1beta1.OtelExporterEndpointSpec{
						Traces: &v1beta1.OtelSignalConfig{
							Endpoint: "http://traces-collector:4318",
						},
						Metrics: &v1beta1.OtelSignalConfig{
							Endpoint: "http://metrics-collector:4318",
						},
					}),
				},
			},
			expectedContains: []string{
				"otlphttp/monitoring-traces",
				"http://traces-collector:4318",
				"otlphttp/monitoring-metrics",
				"http://metrics-collector:4318",
			},
			expectedNotContains: []string{"nop"},
		},
		{
			name: "metrics-only uses nop for traces",
			inputs: []collectorInput{
				{
					Endpoint: endpoint("monitoring", v1beta1.OtelExporterEndpointSpec{
						Metrics: &v1beta1.OtelSignalConfig{
							Endpoint: "http://metrics-collector:4318",
						},
					}),
				},
			},
			expectedContains: []string{
				"otlphttp/monitoring-metrics",
				"http://metrics-collector:4318",
				"nop",
			},
			expectedNotContains: []string{"otlphttp/monitoring-traces"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config, err := generateMergedCollectorConfig(tc.inputs, tc.otelSettings)
			require.NoError(t, err)

			for _, s := range tc.expectedContains {
				require.True(t, strings.Contains(config, s),
					"expected config to contain %q, got:\n%s", s, config)
			}
			for _, s := range tc.expectedNotContains {
				require.False(t, strings.Contains(config, s),
					"expected config NOT to contain %q, got:\n%s", s, config)
			}
		})
	}
}

func TestInferProtocol(t *testing.T) {
	t.Parallel()

	require.Equal(t, "grpc", inferProtocol("grpc://my-collector:4317"))
	require.Equal(t, "http", inferProtocol("http://my-collector:4318"))
	require.Equal(t, "http", inferProtocol("https://support.frmnc.net"))
	require.Equal(t, "http", inferProtocol("my-collector:4318"))
}

func TestIsInsecure(t *testing.T) {
	t.Parallel()

	require.True(t, isInsecure("grpc://my-collector:4317?insecure=true"))
	require.False(t, isInsecure("grpc://my-collector:4317"))
	require.False(t, isInsecure("grpc://my-collector:4317?insecure=false"))
	require.False(t, isInsecure("http://my-collector:4318"))
	require.False(t, isInsecure("https://support.frmnc.net"))
}

func TestStripScheme(t *testing.T) {
	t.Parallel()

	require.Equal(t, "my-collector:4317", stripScheme("grpc://my-collector:4317"))
	require.Equal(t, "http://my-collector:4318", stripScheme("http://my-collector:4318"))
	require.Equal(t, "https://support.frmnc.net", stripScheme("https://support.frmnc.net"))
}

func TestStripSignalPaths(t *testing.T) {
	t.Parallel()

	require.Equal(t, "http://my-collector:4318", stripSignalPaths("http://my-collector:4318/v1/traces"))
	require.Equal(t, "http://my-collector:4318", stripSignalPaths("http://my-collector:4318/v1/metrics"))
	require.Equal(t, "http://my-collector:4318", stripSignalPaths("http://my-collector:4318/v1/traces/"))
	require.Equal(t, "http://my-collector:4318", stripSignalPaths("http://my-collector:4318/v1/metrics/"))
	require.Equal(t, "https://support.frmnc.net", stripSignalPaths("https://support.frmnc.net"))
	require.Equal(t, "http://my-collector:4318/other/path", stripSignalPaths("http://my-collector:4318/other/path"))
	require.Equal(t, "my-collector:4317", stripSignalPaths("my-collector:4317"))
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "my-endpoint", sanitizeName("my-endpoint"))
	require.Contains(t, sanitizeName("my.endpoint"), "my-endpoint-")
	require.NotEqual(t, sanitizeName("my-endpoint"), sanitizeName("my.endpoint"))

	require.NotEqual(t, sanitizeName("a.b-c"), sanitizeName("a-b.c"))
}

func TestEnvSafe(t *testing.T) {
	t.Parallel()

	require.Equal(t, "FORMANCE_SUPPORT", envSafe("formance-support"))
	require.Equal(t, "MY_MONITORING", envSafe("my-monitoring"))
	require.Equal(t, "TEST_123", envSafe("test.123"))
}

func TestBuildCollectorInputs(t *testing.T) {
	t.Parallel()

	endpoints := []v1beta1.OtelExporterEndpoint{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "support"},
			Spec: v1beta1.OtelExporterEndpointSpec{
				Traces: &v1beta1.OtelSignalConfig{
					Endpoint: "https://support.frmnc.net",
					Auth: &v1beta1.OtelExporterAuth{
						Type:       "bearer",
						FromSecret: "formance-license",
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "monitoring"},
			Spec: v1beta1.OtelExporterEndpointSpec{
				Traces: &v1beta1.OtelSignalConfig{
					Endpoint: "http://my-collector:4318",
				},
			},
		},
	}

	inputs, envVars := buildCollectorInputs(endpoints)
	require.Len(t, inputs, 2)
	require.Len(t, envVars, 1)
	require.Equal(t, "AUTH_SUPPORT_TRACES", envVars[0].Name)
	require.Equal(t, "formance-license", envVars[0].ValueFrom.SecretKeyRef.LocalObjectReference.Name)
}
