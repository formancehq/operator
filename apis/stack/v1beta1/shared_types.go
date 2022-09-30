package v1beta1

import (
	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/internal/collectionutil"
)

type ScalingSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	MinReplica int `json:"minReplica,omitempty"`
	// +optional
	MaxReplica int `json:"maxReplica,omitempty"`
	// +optional
	CpuLimit int `json:"cpuLimit,omitempty"`
}

type DatabaseSpec struct {
	// +optional
	Url string `json:"url,omitempty"`
	// +optional
	Type string `json:"type,omitempty"`
}

type IngressConfig struct {
	// +optional
	Enabled *bool `json:"enabled"`
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	Host string `json:"host"`
}

func (cfg *IngressConfig) IsEnabled(configuration *Configuration) bool {
	if cfg == nil || cfg.Enabled == nil {
		return configuration.Ingress.Enabled
	}
	return *cfg.Enabled
}

func (cfg *IngressConfig) Compute(stack *Stack, configuration *Configuration, path string) *IngressSpec {
	if !cfg.IsEnabled(configuration) {
		return nil
	}
	var host string
	if cfg != nil {
		host = cfg.Host
	}
	if host == "" {
		host = stack.Spec.Host
	}

	annotations := configuration.Ingress.Annotations
	if cfg != nil {
		annotations = MergeMaps(annotations, cfg.Annotations)
	}

	return &IngressSpec{
		Path:        path,
		Host:        host,
		Annotations: annotations,
		TLS:         configuration.Ingress.TLS,
	}
}
