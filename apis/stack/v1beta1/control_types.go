package v1beta1

import (
	. "github.com/numary/operator/pkg/apis/v1beta1"
)

// +kubebuilder:object:generate=true
type ControlSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}
