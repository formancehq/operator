package v1beta1

import (
	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/internal/collectionutil"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type PaymentsSpec struct {
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	MongoDB MongoDBConfig  `json:"mongoDB"`
}

func (in *PaymentsSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	return Map(in.MongoDB.Validate(), AddPrefixToFieldError("mongoDB."))
}
