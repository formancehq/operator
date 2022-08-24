package v1beta1

import (
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
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
	Enabled bool `json:"enabled"`
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	Host string `json:"host"`
}

func (cfg *IngressConfig) IsEnabled(stack *Stack) bool {
	if cfg != nil && cfg.Enabled {
		return true
	}
	return stack.Spec.Ingress.Enabled
}

func (cfg *IngressConfig) Compute(stack *Stack, path string) *IngressSpec {
	if !cfg.IsEnabled(stack) {
		return nil
	}
	var host string
	if cfg != nil {
		host = cfg.Host
	}
	if host == "" {
		host = stack.Spec.Ingress.Host
	}

	annotations := stack.Spec.Ingress.Annotations
	if cfg != nil {
		annotations = MergeMaps(annotations, cfg.Annotations)
	}

	return &IngressSpec{
		Path:        path,
		Host:        host,
		Annotations: annotations,
	}
}
