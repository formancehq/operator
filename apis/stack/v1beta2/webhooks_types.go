package v1beta2

import (
	"github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/apis/v1beta2"
	"github.com/numary/operator/pkg/typeutils"
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
	return typeutils.Map(in.MongoDB.Validate(), v1beta1.AddPrefixToFieldError("mongoDB."))
}
