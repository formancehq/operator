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

	collectorServiceName = "otel-collector"
	collectorServicePort = 4318
)

func GetOTELEnvVars(ctx core.Context, stack, serviceName string, sliceStringSeparator string) ([]corev1.EnvVar, error) {
	collectorEndpoint, err := getCollectorEndpoint(ctx, stack)
	if err != nil {
		return nil, err
	}
	if collectorEndpoint != "" {
		return collectorEnvVars(ctx, collectorEndpoint, stack, serviceName, sliceStringSeparator)
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

func getCollectorEndpoint(ctx core.Context, stack string) (string, error) {
	svc := &corev1.Service{}
	err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack,
		Name:      collectorServiceName,
	}, svc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return fmt.Sprintf("%s.%s:%d", collectorServiceName, stack, collectorServicePort), nil
}

func collectorEnvVars(ctx core.Context, collectorEndpoint, stack, serviceName, sliceStringSeparator string) ([]corev1.EnvVar, error) {
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

	return []corev1.EnvVar{
		core.Env("OTEL_TRACES", "true"),
		core.Env("OTEL_TRACES_BATCH", "true"),
		core.Env("OTEL_TRACES_EXPORTER", "otlp"),
		core.Env("OTEL_TRACES_EXPORTER_OTLP_ENDPOINT", collectorEndpoint),
		core.Env("OTEL_TRACES_EXPORTER_OTLP_MODE", "http"),
		core.EnvFromBool("OTEL_TRACES_EXPORTER_OTLP_INSECURE", true),
		core.Env("OTEL_METRICS", "true"),
		core.Env("OTEL_METRICS_BATCH", "true"),
		core.Env("OTEL_METRICS_EXPORTER", "otlp"),
		core.Env("OTEL_METRICS_EXPORTER_OTLP_ENDPOINT", collectorEndpoint),
		core.Env("OTEL_METRICS_EXPORTER_OTLP_MODE", "http"),
		core.EnvFromBool("OTEL_METRICS_EXPORTER_OTLP_INSECURE", true),
		core.Env("OTEL_METRICS_RUNTIME", "true"),
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
	}, nil
}

func HasOpenTelemetryTracesEnabled(ctx core.Context, stack string) (bool, error) {
	collectorEndpoint, err := getCollectorEndpoint(ctx, stack)
	if err != nil {
		return false, err
	}
	if collectorEndpoint != "" {
		return true, nil
	}

	v, err := GetURL(ctx, stack, "opentelemetry", "traces", "dsn")
	if err != nil {
		return false, err
	}

	if v == nil {
		return false, nil
	}

	return true, nil
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
