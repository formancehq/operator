package v1beta2

import (
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	"github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type SearchSpec struct {
	ElasticSearchConfig componentsv1beta1.ElasticSearchConfig `json:"elasticSearch"`

	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`

	//+optional
	Ingress *IngressConfig `json:"ingress"`

	// +optional
	Batching componentsv1beta1.Batching `json:"batching"`
}

func (in *SearchSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	return typeutils.Map(in.ElasticSearchConfig.Validate(), v1beta1.AddPrefixToFieldError("elasticSearch"))
}
