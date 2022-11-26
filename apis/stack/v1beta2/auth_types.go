package v1beta2

import (
	authcomponentsv1beta1 "github.com/numary/operator/apis/auth.components/v1beta1"
	"github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/apis/v1beta2"
	"github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type AuthSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Postgres PostgresConfig `json:"postgres"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	// +optional
	StaticClients []authcomponentsv1beta1.StaticClient `json:"staticClients"`
}

func (in *AuthSpec) Validate() field.ErrorList {
	if in == nil {
		return field.ErrorList{}
	}
	return typeutils.MergeAll(
		typeutils.Map(in.Postgres.Validate(), v1beta1.AddPrefixToFieldError("postgres.")),
	)
}
