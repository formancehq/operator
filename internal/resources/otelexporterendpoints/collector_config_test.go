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
							Auth: &v1beta1.OtelAuthConfig{
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
							Auth: &v1beta1.OtelAuthConfig{
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

func TestStripScheme(t *testing.T) {
	t.Parallel()

	require.Equal(t, "my-collector:4317", stripScheme("grpc://my-collector:4317"))
	require.Equal(t, "http://my-collector:4318", stripScheme("http://my-collector:4318"))
	require.Equal(t, "https://support.frmnc.net", stripScheme("https://support.frmnc.net"))
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
					Auth: &v1beta1.OtelAuthConfig{
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
