package settings

import (
	"fmt"
	"slices"
	"strings"

	"github.com/formancehq/operator/internal/core"
	v1 "k8s.io/api/core/v1"
)

type MonitoringType string

const (
	MonitoringTypeTraces  MonitoringType = "TRACES"
	MonitoringTypeMetrics MonitoringType = "METRICS"
)

func GetOTELEnvVars(ctx core.Context, stack, serviceName string, sliceStringSeparator string) ([]v1.EnvVar, error) {
	return GetOTELEnvVarsWithPrefix(ctx, stack, serviceName, "", sliceStringSeparator)
}

func GetOTELEnvVarsWithPrefix(ctx core.Context, stack, serviceName, prefix, sliceStringSeparator string) ([]v1.EnvVar, error) {

	traces, err := otelEnvVars(ctx, stack, MonitoringTypeTraces, serviceName, prefix, sliceStringSeparator)
	if err != nil {
		return nil, err
	}

	metrics, err := otelEnvVars(ctx, stack, MonitoringTypeMetrics, serviceName, prefix, sliceStringSeparator)
	if err != nil {
		return nil, err
	}
	if len(metrics) > 0 {
		metrics = append(metrics, core.Env(fmt.Sprintf("%sOTEL_METRICS_RUNTIME", prefix), "true"))
	}

	return append(traces, metrics...), nil
}

func HasOpenTelemetryTracesEnabled(ctx core.Context, stack string) (bool, error) {
	v, err := GetURL(ctx, stack, "opentelemetry", "traces", "dsn")
	if err != nil {
		return false, err
	}

	if v == nil {
		return false, nil
	}

	return true, nil
}

func otelEnvVars(ctx core.Context, stack string, monitoringType MonitoringType, serviceName, prefix, sliceStringSeparator string) ([]v1.EnvVar, error) {

	otlp, err := GetURL(ctx, stack, "opentelemetry", strings.ToLower(string(monitoringType)), "dsn")
	if err != nil {
		return nil, err
	}
	if otlp == nil {
		return nil, nil
	}

	ret := []v1.EnvVar{
		core.Env(fmt.Sprintf("%sOTEL_%s", prefix, string(monitoringType)), "true"),
		core.Env(fmt.Sprintf("%sOTEL_%s_BATCH", prefix, string(monitoringType)), "true"),
		core.Env(fmt.Sprintf("%sOTEL_%s_EXPORTER", prefix, string(monitoringType)), "otlp"),
		core.EnvFromBool(fmt.Sprintf("%sOTEL_%s_EXPORTER_OTLP_INSECURE", prefix, string(monitoringType)),
			IsTrue(otlp.Query().Get("insecure"))),
		core.Env(fmt.Sprintf("%sOTEL_%s_EXPORTER_OTLP_MODE", prefix, string(monitoringType)), otlp.Scheme),
		core.Env(fmt.Sprintf("%sOTEL_%s_PORT", prefix, string(monitoringType)), otlp.Port()),
		core.Env(fmt.Sprintf("%sOTEL_%s_ENDPOINT", prefix, string(monitoringType)), otlp.Hostname()),
		core.Env(fmt.Sprintf("%sOTEL_%s_EXPORTER_OTLP_ENDPOINT", prefix, string(monitoringType)), core.ComputeEnvVar("%s:%s",
			fmt.Sprintf("%sOTEL_%s_ENDPOINT", prefix, string(monitoringType)),
			fmt.Sprintf("%sOTEL_%s_PORT", prefix, string(monitoringType)))),
		core.Env(fmt.Sprintf("%sOTEL_SERVICE_NAME", prefix), serviceName),
		{
			Name: "POD_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
	}

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
		fmt.Sprintf("%sOTEL_RESOURCE_ATTRIBUTES", prefix),
		strings.Join(resourceAttributesArray, sliceStringSeparator),
	))

	return ret, nil
}
