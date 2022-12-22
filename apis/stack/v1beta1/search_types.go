package v1beta1

import (
	"github.com/formancehq/operator/apis/components/v1beta2"

	. "github.com/formancehq/operator/pkg/typeutils"
)

// +kubebuilder:object:generate=true
type SearchSpec struct {
	ImageHolder `json:",inline"`

	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`

	// +optional
	ElasticSearchConfig *v1beta2.ElasticSearchConfig `json:"elasticSearch"`

	//+optional
	Ingress *IngressConfig `json:"ingress"`

	// +optional
	Batching v1beta2.Batching `json:"batching"`
}
