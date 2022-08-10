//+kubebuilder:object:generate=true
package sharedtypes

import (
	"fmt"

	"github.com/numary/formance-operator/pkg/envutil"
	"k8s.io/api/core/v1"
)

type MonitoringSpec struct {
	// +optional
	Traces *TracesSpec `json:"traces,omitempty"`
}

func (in *MonitoringSpec) Env() []v1.EnvVar {
	ret := make([]v1.EnvVar, 0)
	if in.Traces != nil {
		ret = append(ret, in.Traces.Env()...)
	}
	return ret
}

type TracesOtlpSpec struct {
	Endpoint string `json:"endpoint,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

func (in *TracesOtlpSpec) Env() []v1.EnvVar {
	return []v1.EnvVar{
		envutil.Env("OTEL_TRACES", "true"),
		envutil.Env("OTEL_TRACES_EXPORTER", "otlp"),
		envutil.Env("OTEL_TRACES_EXPORTER_OTLP_ENDPOINT", in.Endpoint),
		envutil.Env("OTEL_TRACES_EXPORTER_OTLP_INSECURE", fmt.Sprintf("%t", in.Insecure)),
		envutil.Env("OTEL_TRACES_EXPORTER_OTLP_MODE", in.Mode),
	}
}

type TracesSpec struct {
	// +optional
	Otlp *TracesOtlpSpec `json:"otlp,omitempty"`
}

func (in *TracesSpec) Env() []v1.EnvVar {
	ret := make([]v1.EnvVar, 0)
	if in.Otlp != nil {
		ret = append(ret, in.Otlp.Env()...)
	}
	return ret
}
