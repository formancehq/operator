package v1beta1

import (
	. "github.com/formancehq/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type WebhooksSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Debug bool `json:"debug,omitempty"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	// +optional
	MongoDB MongoDBConfig `json:"mongoDB"`
}

func (in *WebhooksSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	return Map(in.MongoDB.Validate(), AddPrefixToFieldError("mongoDB."))
}
