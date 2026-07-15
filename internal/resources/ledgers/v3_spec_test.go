package ledgers

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

	"github.com/formancehq/operator/v3/internal/resources/auths"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func TestComposeLedgerV3ClusterSpec(t *testing.T) {
	t.Parallel()

	base := &ledgerv1alpha1.ClusterSpec{
		Image: ledgerv1alpha1.ImageSpec{PullPolicy: corev1.PullAlways},
		Monitoring: &ledgerv1alpha1.MonitoringConfig{
			Pyroscope:      &ledgerv1alpha1.PyroscopeConfig{Enabled: true, ServerAddress: "http://pyroscope"},
			FlightRecorder: &ledgerv1alpha1.FlightRecorderConfig{Enabled: true, MinAge: "30s"},
			Traces: &ledgerv1alpha1.TracesConfig{
				Sampling: &ledgerv1alpha1.TraceSamplingConfig{Enabled: true, SuccessRatio: "0.1"},
			},
			Metrics: &ledgerv1alpha1.MetricsConfig{KeepInMemory: pointerTo(true)},
			Logs:    &ledgerv1alpha1.LogsConfig{Level: "warn"},
		},
		Auth: &ledgerv1alpha1.AuthorizationConfig{
			ScopeMapping:    map[string][]string{"ledger:read": {"ledger:accounts:read"}},
			AnonymousScopes: []string{"*:read"},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		},
		ExtraEnv: []corev1.EnvVar{
			{Name: "BASE_ONLY", Value: "kept"},
			{Name: "SHARED", Value: "base"},
		},
		PodAnnotations: map[string]string{"base": "kept"},
		AdditionalLabels: map[string]string{
			"base":                       "kept",
			"app.kubernetes.io/name":     "configuration-name",
			"app.kubernetes.io/instance": "configuration-instance",
			ledgerV3PreviewLabel:         "false",
		},
		NodeSelector: map[string]string{"disk": "nvme"},
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{
			MaxSkew:           2,
			TopologyKey:       "configuration.example/failure-domain",
			WhenUnsatisfiable: corev1.ScheduleAnyway,
		}},
		ServiceAccount: ledgerv1alpha1.ServiceAccountSpec{
			Annotations: map[string]string{"eks.amazonaws.com/role-arn": "role"},
		},
	}

	actual := composeLedgerV3ClusterSpec(base, ledgerV3SpecOverrides{
		ImageRepository:  "registry.example/ledger",
		ImageTag:         "v3.0.0",
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry"}},
		Replicas:         5,
		ClusterID:        "stack0",
		Debug:            true,
		TLSSecretName:    "stack0-ledger-v3-tls",
		TLSCAHash:        "ca-hash",
		Preview:          true,
		Resources: &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		},
		ExtraEnv: []corev1.EnvVar{
			{Name: "SHARED", Value: "operator"},
			{Name: "OPERATOR_ONLY", Value: "added"},
		},
		Monitoring: &settings.OpenTelemetryConfiguration{
			ServiceName: "ledger",
			Attributes:  map[string]string{"stack": "stack0", "pod-name": "$(POD_NAME)"},
			Traces:      &settings.OpenTelemetrySignalConfiguration{Endpoint: "otel", Port: "4318", Insecure: true, Mode: "http"},
			Metrics:     &settings.OpenTelemetrySignalConfiguration{Endpoint: "otel", Port: "4318", Insecure: true, Mode: "http"},
			Logs:        &settings.OpenTelemetrySignalConfiguration{Endpoint: "logs", Port: "4317", Mode: "grpc"},
		},
		Auth: &auths.ProtectedAuthConfiguration{
			Issuer:               "https://auth.example",
			Issuers:              []string{"https://issuer.example"},
			ReadKeySetMaxRetries: 7,
			CheckScopes:          true,
			Service:              "ledger",
		},
		ServiceAccountName:        "ledger-aws",
		TopologySpreadConstraints: pointerTo(true),
	})

	require.Equal(t, "registry.example/ledger", actual.Image.Repository)
	require.Equal(t, "v3.0.0", actual.Image.Tag)
	require.Equal(t, corev1.PullAlways, actual.Image.PullPolicy)
	require.Equal(t, int32(5), *actual.Replicas)
	require.Equal(t, "stack0", actual.ClusterID)
	require.True(t, actual.Debug)
	require.Equal(t, "stack0-ledger-v3-tls", actual.TLS.SecretName)
	require.Equal(t, "ca-hash", actual.PodAnnotations[ledgerV3TLSCAHashAnnotation])
	require.Equal(t, "kept", actual.PodAnnotations["base"])
	require.Equal(t, "kept", actual.AdditionalLabels["base"])
	require.Equal(t, "ledger-v3-preview", actual.AdditionalLabels["app.kubernetes.io/name"])
	require.Equal(t, "stack0", actual.AdditionalLabels["app.kubernetes.io/instance"])
	require.Equal(t, "true", actual.AdditionalLabels[ledgerV3PreviewLabel])
	require.Equal(t, "nvme", actual.NodeSelector["disk"])
	require.Equal(t, defaultLedgerV3TopologySpreadConstraints(), actual.TopologySpreadConstraints)
	require.Equal(t, "2", actual.Resources.Limits.Cpu().String())
	require.Empty(t, actual.Resources.Requests)
	require.Equal(t, []corev1.EnvVar{
		{Name: "BASE_ONLY", Value: "kept"},
		{Name: "OPERATOR_ONLY", Value: "added"},
		{Name: "SHARED", Value: "operator"},
	}, actual.ExtraEnv)

	require.True(t, actual.Monitoring.Pyroscope.Enabled)
	require.Equal(t, "30s", actual.Monitoring.FlightRecorder.MinAge)
	require.Equal(t, "0.1", actual.Monitoring.Traces.Sampling.SuccessRatio)
	require.True(t, *actual.Monitoring.Metrics.KeepInMemory)
	require.Equal(t, "warn", actual.Monitoring.Logs.Level)
	require.Equal(t, "ledger", actual.Monitoring.ServiceName)
	require.Equal(t, "pod-name=$(POD_NAME),stack=stack0", actual.Monitoring.Attributes)
	require.Equal(t, "otel", actual.Monitoring.Traces.Endpoint)
	require.Equal(t, "true", actual.Monitoring.Traces.Insecure)
	require.Equal(t, "true", actual.Monitoring.Traces.Batch)
	require.True(t, *actual.Monitoring.Metrics.Runtime)

	require.Equal(t, []string{"*:read"}, actual.Auth.AnonymousScopes)
	require.Equal(t, []string{"ledger:accounts:read"}, actual.Auth.ScopeMapping["ledger:read"])
	require.Equal(t, "https://auth.example", actual.Auth.Issuer)
	require.True(t, *actual.Auth.Enabled)
	require.True(t, *actual.Auth.CheckScopes)
	require.Equal(t, int32(7), *actual.Auth.ReadKeySetMaxRetries)
	require.Equal(t, "ledger-aws", actual.ServiceAccount.Name)
	require.False(t, *actual.ServiceAccount.Create)
	require.Equal(t, "role", actual.ServiceAccount.Annotations["eks.amazonaws.com/role-arn"])

	// Composition must not mutate the shared configuration stored in Kubernetes.
	require.Equal(t, "base", base.ExtraEnv[1].Value)
	require.Empty(t, base.Image.Repository)
}

func TestComposeLedgerV3ClusterSpecPreservesOptionalBaseValues(t *testing.T) {
	t.Parallel()

	base := &ledgerv1alpha1.ClusterSpec{
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: "base-registry"}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
		},
		Auth: &ledgerv1alpha1.AuthorizationConfig{Enabled: pointerTo(true), Issuer: "https://external.example"},
		Monitoring: &ledgerv1alpha1.MonitoringConfig{
			ServiceName: "configuration-service",
			Pyroscope:   &ledgerv1alpha1.PyroscopeConfig{Enabled: true},
		},
		AdditionalLabels: map[string]string{
			"app.kubernetes.io/name":     "configuration-name",
			"app.kubernetes.io/instance": "configuration-instance",
			ledgerV3PreviewLabel:         "true",
		},
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{
			MaxSkew:           2,
			TopologyKey:       "configuration.example/failure-domain",
			WhenUnsatisfiable: corev1.ScheduleAnyway,
		}},
	}
	actual := composeLedgerV3ClusterSpec(base, ledgerV3SpecOverrides{
		ImageRepository: "ledger",
		ImageTag:        "latest",
		Replicas:        3,
		ClusterID:       "stack0",
		TLSSecretName:   "tls",
	})

	require.Equal(t, "base-registry", actual.ImagePullSecrets[0].Name)
	require.Equal(t, "512Mi", actual.Resources.Requests.Memory().String())
	require.Equal(t, "https://external.example", actual.Auth.Issuer)
	require.Equal(t, base.TopologySpreadConstraints, actual.TopologySpreadConstraints)
	require.Equal(t, "ledger", actual.Monitoring.ServiceName)
	require.True(t, actual.Monitoring.Pyroscope.Enabled)
	require.Equal(t, "ledger", actual.AdditionalLabels["app.kubernetes.io/name"])
	require.Equal(t, "stack0", actual.AdditionalLabels["app.kubernetes.io/instance"])
	require.NotContains(t, actual.AdditionalLabels, ledgerV3PreviewLabel)
}

func TestComposeLedgerV3ClusterSpecDisablesConfiguredTopologySpreadConstraints(t *testing.T) {
	t.Parallel()

	base := &ledgerv1alpha1.ClusterSpec{
		TopologySpreadConstraints: defaultLedgerV3TopologySpreadConstraints(),
	}
	actual := composeLedgerV3ClusterSpec(base, ledgerV3SpecOverrides{
		ImageRepository:           "ledger",
		ImageTag:                  "latest",
		Replicas:                  3,
		ClusterID:                 "stack0",
		TLSSecretName:             "tls",
		TopologySpreadConstraints: pointerTo(false),
	})

	require.Nil(t, actual.TopologySpreadConstraints)
	require.NotEmpty(t, base.TopologySpreadConstraints)
}
