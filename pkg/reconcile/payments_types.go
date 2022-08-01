package reconcile

// +kubebuilder:object:generate=true
type PaymentsSpec struct {
	// +required
	Name string `json:"name,omitempty"`
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Databases []DatabaseSpec `json:"databases,omitempty"`
}
