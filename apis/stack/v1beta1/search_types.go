package v1beta1

import (
	"github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type SearchSpec struct {
	ImageHolder `json:",inline"`

	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`

	// +optional
	ElasticSearchConfig *v1beta1.ElasticSearchConfig `json:"elasticSearch"`

	//+optional
	Ingress *IngressConfig `json:"ingress"`

	// +optional
	Batching v1beta1.Batching `json:"batching"`
}

func (in *SearchSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	return Map(in.ElasticSearchConfig.Validate(), AddPrefixToFieldError("elasticSearch"))
}
