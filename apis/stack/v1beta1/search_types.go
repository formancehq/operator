package v1beta1

import (
	"github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/internal/collectionutil"
	"k8s.io/apimachinery/pkg/util/validation/field"
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

func (in *SearchSpec) Validate() field.ErrorList {
	return Map(in.ElasticSearchConfig.Validate(), AddPrefixToFieldError("elasticSearch"))
}
