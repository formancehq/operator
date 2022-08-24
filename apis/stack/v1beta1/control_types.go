package v1beta1

import (
	. "github.com/numary/formance-operator/apis/sharedtypes"
)

// +kubebuilder:object:generate=true
type ControlSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Debug bool `json:"debug"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}
