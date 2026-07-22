package ledgers

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/auths"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

type ledgerV3SpecOverrides struct {
	ImageRepository           string
	ImageTag                  string
	ImagePullSecrets          []corev1.LocalObjectReference
	Replicas                  int32
	ClusterID                 string
	Debug                     bool
	TLSSecretName             string
	TLSCAHash                 string
	Preview                   bool
	Resources                 *corev1.ResourceRequirements
	ExtraEnv                  []corev1.EnvVar
	Monitoring                *settings.OpenTelemetryConfiguration
	Auth                      *auths.ProtectedAuthConfiguration
	ServiceAccountName        string
	TopologySpreadConstraints *bool
}

// composeLedgerV3ClusterSpec applies the values owned by the Formance Operator
// to a shared ClusterSpec. Unmanaged fields remain entirely owned by the
// LedgerConfiguration.
func composeLedgerV3ClusterSpec(base *ledgerv1alpha1.ClusterSpec, overrides ledgerV3SpecOverrides) (*ledgerv1alpha1.ClusterSpec, error) {
	spec := base.DeepCopy()

	spec.Image.Repository = overrides.ImageRepository
	spec.Image.Tag = overrides.ImageTag
	if len(overrides.ImagePullSecrets) > 0 {
		spec.ImagePullSecrets = slices.Clone(overrides.ImagePullSecrets)
	}
	spec.Replicas = pointerTo(overrides.Replicas)
	spec.ClusterID = overrides.ClusterID
	spec.Debug = overrides.Debug
	// TLS is configured from the first Cluster revision. Pods may wait for the
	// cert-manager Secret, but must never bootstrap a plaintext Raft cluster.
	spec.TLS = &ledgerv1alpha1.TLSConfig{
		Enabled:     true,
		SecretName:  overrides.TLSSecretName,
		CASecretKey: ledgerV3TLSCASecretKey,
	}

	if spec.PodAnnotations == nil {
		spec.PodAnnotations = map[string]string{}
	}
	if overrides.TLSCAHash != "" {
		spec.PodAnnotations[ledgerV3TLSCAHashAnnotation] = overrides.TLSCAHash
	} else {
		delete(spec.PodAnnotations, ledgerV3TLSCAHashAnnotation)
	}
	if len(spec.PodAnnotations) == 0 {
		spec.PodAnnotations = nil
	}

	if overrides.Preview {
		if spec.AdditionalLabels == nil {
			spec.AdditionalLabels = map[string]string{}
		}
		spec.AdditionalLabels["app.kubernetes.io/name"] = "ledger-v3-preview"
		spec.AdditionalLabels[ledgerV3PreviewLabel] = "true"
	} else {
		if spec.AdditionalLabels == nil {
			spec.AdditionalLabels = map[string]string{}
		}
		spec.AdditionalLabels["app.kubernetes.io/name"] = "ledger"
		delete(spec.AdditionalLabels, ledgerV3PreviewLabel)
	}
	// The Ledger Operator deliberately allows these labels to override its
	// selectors. Keep the instance label aligned with the Cluster name because
	// Formance Services and NetworkPolicies rely on that stable selector.
	spec.AdditionalLabels["app.kubernetes.io/instance"] = overrides.ClusterID

	if hasResourceRequirements(overrides.Resources) {
		spec.Resources = *overrides.Resources.DeepCopy()
	}
	if len(overrides.ExtraEnv) > 0 {
		spec.ExtraEnv = core.MergeEnvVars(spec.ExtraEnv, overrides.ExtraEnv)
	}
	applyLedgerV3Monitoring(spec, overrides.Monitoring)
	if err := applyLedgerV3Auth(spec, overrides.Auth); err != nil {
		return nil, err
	}

	if overrides.ServiceAccountName != "" {
		spec.ServiceAccount.Create = pointerTo(false)
		spec.ServiceAccount.Name = overrides.ServiceAccountName
	}
	if overrides.TopologySpreadConstraints != nil {
		if *overrides.TopologySpreadConstraints {
			spec.TopologySpreadConstraints = defaultLedgerV3TopologySpreadConstraints()
		} else {
			spec.TopologySpreadConstraints = nil
		}
	}

	return spec, nil
}

func defaultLedgerV3TopologySpreadConstraints() []corev1.TopologySpreadConstraint {
	return []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           1,
			TopologyKey:       corev1.LabelTopologyZone,
			WhenUnsatisfiable: corev1.ScheduleAnyway,
		},
		{
			MaxSkew:           1,
			TopologyKey:       corev1.LabelHostname,
			WhenUnsatisfiable: corev1.DoNotSchedule,
		},
	}
}

func applyLedgerV3Auth(spec *ledgerv1alpha1.ClusterSpec, configuration *auths.ProtectedAuthConfiguration) error {
	if configuration == nil {
		return nil
	}
	if spec.Auth == nil {
		spec.Auth = &ledgerv1alpha1.AuthorizationConfig{}
	}
	spec.Auth.Enabled = pointerTo(true)
	spec.Auth.Issuer = configuration.Issuer
	if len(configuration.Issuers) > 0 {
		spec.Auth.Issuers = slices.Clone(configuration.Issuers)
	}
	spec.Auth.CheckScopes = pointerTo(configuration.CheckScopes)
	if configuration.CheckScopes {
		spec.Auth.Service = configuration.Service
	}
	if configuration.ReadKeySetMaxRetries != 0 {
		if configuration.ReadKeySetMaxRetries > math.MaxInt32 || configuration.ReadKeySetMaxRetries < math.MinInt32 {
			return fmt.Errorf("auth read key set max retries must fit in int32, got %d", configuration.ReadKeySetMaxRetries)
		}
		spec.Auth.ReadKeySetMaxRetries = pointerTo(int32(configuration.ReadKeySetMaxRetries))
	}
	return nil
}

func applyLedgerV3Monitoring(spec *ledgerv1alpha1.ClusterSpec, configuration *settings.OpenTelemetryConfiguration) {
	if spec.Monitoring != nil {
		spec.Monitoring.ServiceName = "ledger"
	}
	if configuration == nil {
		return
	}
	if spec.Monitoring == nil {
		spec.Monitoring = &ledgerv1alpha1.MonitoringConfig{}
	}
	spec.Monitoring.ServiceName = "ledger"
	if len(configuration.Attributes) > 0 {
		attributes := make([]string, 0, len(configuration.Attributes))
		for key, value := range configuration.Attributes {
			attributes = append(attributes, key+"="+value)
		}
		slices.Sort(attributes)
		spec.Monitoring.Attributes = strings.Join(attributes, ",")
	}
	if configuration.Traces != nil {
		if spec.Monitoring.Traces == nil {
			spec.Monitoring.Traces = &ledgerv1alpha1.TracesConfig{}
		}
		applyLedgerV3MonitoringSignal(configuration.Traces,
			&spec.Monitoring.Traces.Enabled,
			&spec.Monitoring.Traces.Exporter,
			&spec.Monitoring.Traces.Endpoint,
			&spec.Monitoring.Traces.Port,
			&spec.Monitoring.Traces.Insecure,
			&spec.Monitoring.Traces.Mode)
		spec.Monitoring.Traces.Batch = "true"
	}
	if configuration.Metrics != nil {
		if spec.Monitoring.Metrics == nil {
			spec.Monitoring.Metrics = &ledgerv1alpha1.MetricsConfig{}
		}
		applyLedgerV3MonitoringSignal(configuration.Metrics,
			&spec.Monitoring.Metrics.Enabled,
			&spec.Monitoring.Metrics.Exporter,
			&spec.Monitoring.Metrics.Endpoint,
			&spec.Monitoring.Metrics.Port,
			&spec.Monitoring.Metrics.Insecure,
			&spec.Monitoring.Metrics.Mode)
		spec.Monitoring.Metrics.Runtime = pointerTo(true)
	}
	if configuration.Logs != nil {
		if spec.Monitoring.Logs == nil {
			spec.Monitoring.Logs = &ledgerv1alpha1.LogsConfig{}
		}
		applyLedgerV3MonitoringSignal(configuration.Logs,
			&spec.Monitoring.Logs.Enabled,
			&spec.Monitoring.Logs.Exporter,
			&spec.Monitoring.Logs.Endpoint,
			&spec.Monitoring.Logs.Port,
			&spec.Monitoring.Logs.Insecure,
			&spec.Monitoring.Logs.Mode)
	}
}

func applyLedgerV3MonitoringSignal(
	configuration *settings.OpenTelemetrySignalConfiguration,
	enabled **bool,
	exporter, endpoint, port, insecure, mode *string,
) {
	*enabled = pointerTo(true)
	*exporter = "otlp"
	*endpoint = configuration.Endpoint
	*port = configuration.Port
	*insecure = strconv.FormatBool(configuration.Insecure)
	*mode = configuration.Mode
}

func pointerTo[T any](value T) *T {
	return &value
}
