package v1beta2

import (
	"github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type PaymentsSpec struct {
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	// +optional
	Postgres v1beta1.PostgresConfig `json:"postgres"`
}

func (in *PaymentsSpec) Validate() field.ErrorList {
	if in == nil {
		return field.ErrorList{}
	}
	return typeutils.MergeAll(
		typeutils.Map(in.Postgres.Validate(), v1beta1.AddPrefixToFieldError("postgres.")),
	)
}
