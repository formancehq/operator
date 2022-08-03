package v1beta1

// +kubebuilder:object:generate=true
type LedgerSpec struct {
	// +required
	Name string `json:"name,omitempty"`
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Databases []DatabaseSpec `json:"databases,omitempty"`
	// +optional
	// +kubebuilder:default=80
	Port int `json:"port,omitempty"`
}
