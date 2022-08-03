package v1beta1

// +kubebuilder:object:generate=true
type SearchSpec struct {
	// +required
	Name string `json:"name,omitempty"`
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Databases []DatabaseSpec `json:"databases,omitempty"`
}
