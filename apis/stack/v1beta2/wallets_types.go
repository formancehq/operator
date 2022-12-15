package v1beta2

import (
	"github.com/numary/operator/pkg/apis/v1beta2"
	"github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type WalletsSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Debug bool `json:"debug,omitempty"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	// +optional
	Postgres v1beta2.PostgresConfig `json:"postgres"`
}

func (in *WalletsSpec) Validate() field.ErrorList {
	if in == nil {
		return field.ErrorList{}
	}
	return typeutils.MergeAll(
		typeutils.Map(in.Postgres.Validate(), v1beta2.AddPrefixToFieldError("postgres.")),
	)
}
