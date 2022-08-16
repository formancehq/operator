package v1beta1

// +kubebuilder:object:generate=true
type ControlSpec struct {
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Debug bool `json:"debug"`
	// +optional
	Image string `json:"image"`
}
