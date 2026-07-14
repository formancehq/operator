package settings

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/internal/core"
)

type MonitoringType string

const (
	MonitoringTypeTraces  MonitoringType = "TRACES"
	MonitoringTypeMetrics MonitoringType = "METRICS"
	MonitoringTypeLogs    MonitoringType = "LOGS"

	collectorServiceName = "otel-collector"
	collectorServicePort = 4318
)

type OpenTelemetrySignalConfiguration struct {
	Endpoint string
	Port     string
	Insecure bool
	Mode     string
}

type OpenTelemetryConfiguration struct {
	ServiceName string
	Attributes  map[string]string
	Traces      *OpenTelemetrySignalConfiguration
	Metrics     *OpenTelemetrySignalConfiguration
	Logs        *OpenTelemetrySignalConfiguration
}

func GetOpenTelemetryConfiguration(ctx core.Context, stack, serviceName string) (*OpenTelemetryConfiguration, error) {
	info, err := getCollectorInfo(ctx, stack)
	if err != nil {
		return nil, err
	}

	configuration := &OpenTelemetryConfiguration{
		ServiceName: serviceName,
		Attributes: map[string]string{
			"pod-name": "$(POD_NAME)",
			"stack":    stack,
		},
	}
	if info != nil {
		endpoint, port, _ := strings.Cut(info.endpoint, ":")
		newSignal := func() *OpenTelemetrySignalConfiguration {
			return &OpenTelemetrySignalConfiguration{
				Endpoint: endpoint,
				Port:     port,
				Insecure: true,
				Mode:     "http",
			}
		}
		if info.hasTraces {
			configuration.Traces = newSignal()
		}
		if info.hasMetrics {
			configuration.Metrics = newSignal()
		}
		for _, signal := range []string{"traces", "metrics"} {
			attributes, err := GetMap(ctx, stack, "opentelemetry", signal, "resource-attributes")
			if err != nil {
				return nil, err
			}
			for key, value := range attributes {
				configuration.Attributes[key] = value
			}
		}
		configuration.Logs, err = getConfiguredOpenTelemetrySignal(ctx, stack, "logs", configuration.Attributes)
		if err != nil {
			return nil, err
		}
		if configuration.Traces == nil && configuration.Metrics == nil && configuration.Logs == nil {
			return nil, nil
		}
		return configuration, nil
	}

	for _, signal := range []struct {
		name   string
		target **OpenTelemetrySignalConfiguration
	}{
		{name: "traces", target: &configuration.Traces},
		{name: "metrics", target: &configuration.Metrics},
		{name: "logs", target: &configuration.Logs},
	} {
		*signal.target, err = getConfiguredOpenTelemetrySignal(ctx, stack, signal.name, configuration.Attributes)
		if err != nil {
			return nil, err
		}
	}

	if configuration.Traces == nil && configuration.Metrics == nil && configuration.Logs == nil {
		return nil, nil
	}
	return configuration, nil
}

func getConfiguredOpenTelemetrySignal(ctx core.Context, stack, signal string, attributesTarget map[string]string) (*OpenTelemetrySignalConfiguration, error) {
	dsn, err := GetURL(ctx, stack, "opentelemetry", signal, "dsn")
	if err != nil || dsn == nil {
		return nil, err
	}

	attributes, err := GetMap(ctx, stack, "opentelemetry", signal, "resource-attributes")
	if err != nil {
		return nil, err
	}
	for key, value := range attributes {
		attributesTarget[key] = value
	}

	return &OpenTelemetrySignalConfiguration{
		Endpoint: dsn.Hostname(),
		Port:     dsn.Port(),
		Insecure: IsTrue(dsn.Query().Get("insecure")),
		Mode:     dsn.Scheme,
	}, nil
}

func GetOTELEnvVars(ctx core.Context, stack, serviceName string, sliceStringSeparator string) ([]corev1.EnvVar, error) {
	info, err := getCollectorInfo(ctx, stack)
	if err != nil {
		return nil, err
	}
	if info != nil {
		return collectorEnvVars(ctx, info, stack, serviceName, sliceStringSeparator)
	}

	traces, err := otelEnvVars(ctx, stack, MonitoringTypeTraces, serviceName, sliceStringSeparator)
	if err != nil {
		return nil, err
	}

	metrics, err := otelEnvVars(ctx, stack, MonitoringTypeMetrics, serviceName, sliceStringSeparator)
	if err != nil {
		return nil, err
	}
	if len(metrics) > 0 {
		metrics = append(metrics, core.Env("OTEL_METRICS_RUNTIME", "true"))
	}

	return append(traces, metrics...), nil
}

type collectorInfo struct {
	endpoint   string
	hasTraces  bool
	hasMetrics bool
}

func getCollectorInfo(ctx core.Context, stack string) (*collectorInfo, error) {
	svc := &corev1.Service{}
	err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack,
		Name:      collectorServiceName,
	}, svc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if svc.Labels[core.CollectorManagedByLabel] != core.CollectorManagedByValue {
		return nil, nil
	}
	return &collectorInfo{
		endpoint:   fmt.Sprintf("%s.%s:%d", collectorServiceName, stack, collectorServicePort),
		hasTraces:  svc.Annotations[core.SignalTracesAnnotation] == "true",
		hasMetrics: svc.Annotations[core.SignalMetricsAnnotation] == "true",
	}, nil
}

func collectorEnvVars(ctx core.Context, info *collectorInfo, stack, serviceName, sliceStringSeparator string) ([]corev1.EnvVar, error) {
	resourceAttributes := map[string]string{}
	for _, signal := range []string{"traces", "metrics"} {
		attrs, err := GetMap(ctx, stack, "opentelemetry", signal, "resource-attributes")
		if err != nil {
			return nil, err
		}
		for k, v := range attrs {
			resourceAttributes[k] = v
		}
	}
	resourceAttributes["stack"] = stack
	resourceAttributes["pod-name"] = "$(POD_NAME)"

	resourceAttributesArray := make([]string, 0, len(resourceAttributes))
	for k, v := range resourceAttributes {
		resourceAttributesArray = append(resourceAttributesArray, fmt.Sprintf("%s=%s", k, v))
	}
	slices.Sort(resourceAttributesArray)

	envVars := []corev1.EnvVar{
		core.Env("OTEL_SERVICE_NAME", serviceName),
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		core.Env("OTEL_RESOURCE_ATTRIBUTES", strings.Join(resourceAttributesArray, sliceStringSeparator)),
	}

	if info.hasTraces {
		envVars = append(envVars,
			core.Env("OTEL_TRACES", "true"),
			core.Env("OTEL_TRACES_BATCH", "true"),
			core.Env("OTEL_TRACES_EXPORTER", "otlp"),
			core.Env("OTEL_TRACES_EXPORTER_OTLP_ENDPOINT", info.endpoint),
			core.Env("OTEL_TRACES_EXPORTER_OTLP_MODE", "http"),
			core.EnvFromBool("OTEL_TRACES_EXPORTER_OTLP_INSECURE", true),
		)
	}

	if info.hasMetrics {
		envVars = append(envVars,
			core.Env("OTEL_METRICS", "true"),
			core.Env("OTEL_METRICS_BATCH", "true"),
			core.Env("OTEL_METRICS_EXPORTER", "otlp"),
			core.Env("OTEL_METRICS_EXPORTER_OTLP_ENDPOINT", info.endpoint),
			core.Env("OTEL_METRICS_EXPORTER_OTLP_MODE", "http"),
			core.EnvFromBool("OTEL_METRICS_EXPORTER_OTLP_INSECURE", true),
			core.Env("OTEL_METRICS_RUNTIME", "true"),
		)
	}

	return envVars, nil
}

func HasOpenTelemetryTracesEnabled(ctx core.Context, stack string) (bool, error) {
	info, err := getCollectorInfo(ctx, stack)
	if err != nil {
		return false, err
	}
	if info != nil {
		return info.hasTraces, nil
	}

	v, err := GetURL(ctx, stack, "opentelemetry", "traces", "dsn")
	if err != nil {
		return false, err
	}

	return v != nil, nil
}

func otelEnvVars(ctx core.Context, stack string, monitoringType MonitoringType, serviceName, sliceStringSeparator string) ([]corev1.EnvVar, error) {

	otlp, err := GetURL(ctx, stack, "opentelemetry", strings.ToLower(string(monitoringType)), "dsn")
	if err != nil {
		return nil, err
	}
	if otlp == nil {
		return nil, nil
	}

	ret := []corev1.EnvVar{
		core.Env(fmt.Sprintf("OTEL_%s", string(monitoringType)), "true"),
		core.Env(fmt.Sprintf("OTEL_%s_BATCH", string(monitoringType)), "true"),
		core.Env(fmt.Sprintf("OTEL_%s_EXPORTER", string(monitoringType)), "otlp"),
		core.EnvFromBool(fmt.Sprintf("OTEL_%s_EXPORTER_OTLP_INSECURE", string(monitoringType)), IsTrue(otlp.Query().Get("insecure"))),
		core.Env("OTEL_SERVICE_NAME", serviceName),
		core.Env(fmt.Sprintf("OTEL_%s_EXPORTER_OTLP_MODE", string(monitoringType)), otlp.Scheme),
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
	}

	// If the path is not empty, we use the full URL as the endpoint.
	var otlpEndpoint corev1.EnvVar
	otlpEndpointEnvName := fmt.Sprintf("OTEL_%s_EXPORTER_OTLP_ENDPOINT", string(monitoringType))
	if otlp.Path != "" {
		otlpEndpoint = core.Env(otlpEndpointEnvName, otlp.String())
	} else {
		ret = append(ret, core.Env(fmt.Sprintf("OTEL_%s_PORT", string(monitoringType)), otlp.Port()))
		ret = append(ret, core.Env(fmt.Sprintf("OTEL_%s_ENDPOINT", string(monitoringType)), otlp.Hostname()))
		otlpEndpoint = core.Env(
			otlpEndpointEnvName,
			core.ComputeEnvVar(
				"%s:%s",
				fmt.Sprintf("OTEL_%s_ENDPOINT", string(monitoringType)),
				fmt.Sprintf("OTEL_%s_PORT", string(monitoringType)),
			),
		)
	}
	ret = append(ret, otlpEndpoint)

	resourceAttributes, err := GetMap(ctx, stack, "opentelemetry", strings.ToLower(string(monitoringType)), "resource-attributes")
	if err != nil {
		return nil, err
	}

	if resourceAttributes == nil {
		resourceAttributes = map[string]string{}
	}
	resourceAttributes["stack"] = stack
	resourceAttributes["pod-name"] = "$(POD_NAME)"

	resourceAttributesArray := make([]string, 0)
	for k, v := range resourceAttributes {
		resourceAttributesArray = append(resourceAttributesArray, fmt.Sprintf("%s=%s", k, v))
	}
	slices.Sort(resourceAttributesArray)

	ret = append(ret, core.Env(
		"OTEL_RESOURCE_ATTRIBUTES",
		strings.Join(resourceAttributesArray, sliceStringSeparator),
	))

	return ret, nil
}
