// +kubebuilder:object:generate=true
package sharedtypes

import (
	"fmt"

	"github.com/numary/formance-operator/internal/envutil"
	v1 "k8s.io/api/core/v1"
)

type MonitoringSpec struct {
	// +optional
	Traces *TracesSpec `json:"traces,omitempty"`
}

func (in *MonitoringSpec) Env(prefix string) []v1.EnvVar {
	ret := make([]v1.EnvVar, 0)
	if in.Traces != nil {
		ret = append(ret, in.Traces.Env(prefix)...)
	}
	return ret
}

type EndpointReference struct {
	// +optional
	Value string `json:"value,omitempty" protobuf:"bytes,2,opt,name=value"`
	// +optional
	ValueFrom *v1.EnvVarSource `json:"valueFrom,omitempty" protobuf:"bytes,3,opt,name=valueFrom"`
}

type TracesOtlpSpec struct {
	Endpoint EndpointReference `json:"endpoint,omitempty"`
	// +optional
	Port     int32  `json:"port"`
	Insecure bool   `json:"insecure,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

func (in *TracesOtlpSpec) Env(prefix string) []v1.EnvVar {
	env := []v1.EnvVar{
		envutil.EnvWithPrefix(prefix, "OTEL_TRACES", "true"),
		envutil.EnvWithPrefix(prefix, "OTEL_TRACES_EXPORTER", "otlp"),
		envutil.EnvWithPrefix(prefix, "OTEL_TRACES_EXPORTER_OTLP_INSECURE", fmt.Sprintf("%t", in.Insecure)),
		envutil.EnvWithPrefix(prefix, "OTEL_TRACES_EXPORTER_OTLP_MODE", in.Mode),
		envutil.Env("PORT", fmt.Sprintf("%d", in.Port)),
	}
	switch {
	case in.Endpoint.Value != "":
		env = append(env, envutil.Env("ENDPOINT", in.Endpoint.Value))
	case in.Endpoint.ValueFrom != nil:
		env = append(env, envutil.EnvFrom("ENDPOINT", in.Endpoint.ValueFrom))
	}
	env = append(env, envutil.EnvWithPrefix(prefix, "OTEL_TRACES_EXPORTER_OTLP_ENDPOINT", "$(ENDPOINT):$(PORT)"))
	return env
}

type TracesSpec struct {
	// +optional
	Otlp *TracesOtlpSpec `json:"otlp,omitempty"`
}

func (in *TracesSpec) Env(prefix string) []v1.EnvVar {
	ret := make([]v1.EnvVar, 0)
	if in.Otlp != nil {
		ret = append(ret, in.Otlp.Env(prefix)...)
	}
	return ret
}
