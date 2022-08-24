package v1beta1

import (
	"github.com/numary/formance-operator/apis/components/v1beta1"
)

// +kubebuilder:object:generate=true
type SearchSpec struct {
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Image string `json:"image"`
	// +optional
	Debug bool `json:"debug"`

	ElasticSearchConfig *v1beta1.ElasticSearchConfig `json:"elasticSearch"`
	//+optional
	Ingress *IngressConfig `json:"ingress"`
}
