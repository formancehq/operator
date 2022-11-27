package v1beta1

import (
	. "github.com/formancehq/operator/apis/sharedtypes"
)

// +kubebuilder:object:generate=true
type ControlSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}
