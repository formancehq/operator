package v1beta1

import (
	"github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
)

// +kubebuilder:object:generate=true
type SearchSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Debug bool `json:"debug"`

	ElasticSearchConfig *v1beta1.ElasticSearchConfig `json:"elasticSearch"`
	//+optional
	Ingress *IngressConfig `json:"ingress"`
}
