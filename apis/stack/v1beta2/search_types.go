package v1beta2

import (
	"github.com/numary/operator/apis/components/v1beta2"
	"github.com/numary/operator/pkg/apis/v1beta1"
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	"github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type SearchSpec struct {
	apisv1beta2.ImageHolder `json:",inline"`

	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`

	// +optional
	ElasticSearchConfig *v1beta2.ElasticSearchConfig `json:"elasticSearch"`

	//+optional
	Ingress *IngressConfig `json:"ingress"`

	// +optional
	Batching v1beta2.Batching `json:"batching"`
}

func (in *SearchSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	return typeutils.Map(in.ElasticSearchConfig.Validate(), v1beta1.AddPrefixToFieldError("elasticSearch"))
}
