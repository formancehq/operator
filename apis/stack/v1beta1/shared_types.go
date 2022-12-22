package v1beta1

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

func (cfg *IngressConfig) IsEnabled(configuration *ConfigurationSpec) bool {
	if cfg == nil || cfg.Enabled == nil {
		return configuration.Ingress.Enabled
	}
	return *cfg.Enabled
}

type IngressSpec struct {
	Path        string
	Host        string
	Annotations map[string]string
	TLS         string
}
