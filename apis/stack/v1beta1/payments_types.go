package v1beta1

import (
	. "github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type PaymentsSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	// +optional
	MongoDB MongoDBConfig `json:"mongoDB"`
}

func (in *PaymentsSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	return Map(in.MongoDB.Validate(), AddPrefixToFieldError("mongoDB."))
}
