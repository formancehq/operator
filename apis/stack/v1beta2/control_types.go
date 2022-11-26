package v1beta2

import (
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
)

// +kubebuilder:object:generate=true
type ControlSpec struct {
	apisv1beta2.ImageHolder `json:",inline"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}
