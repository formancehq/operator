package v1beta1

import (
	authv1beta1 "github.com/numary/operator/apis/auth.components/v1beta1"
	"github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type AuthSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Postgres PostgresConfig `json:"postgres"`
	// +optional
	SigningKey string `json:"signingKey"`
	// +optional
	DelegatedOIDCServer *v1beta1.DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	// +optional
	Host string `json:"host,omitempty"`
	// +optional
	Scheme string `json:"scheme,omitempty"`
	// +optional
	StaticClients []authv1beta1.StaticClient `json:"staticClients"`
}

func (in *AuthSpec) GetScheme() string {
	if in.Scheme != "" {
		return in.Scheme
	}
	return "https"
}

func (in *AuthSpec) Validate() field.ErrorList {
	if in == nil {
		return field.ErrorList{}
	}
	return MergeAll(
		Map(in.Postgres.Validate(), AddPrefixToFieldError("postgres.")),
		Map(in.DelegatedOIDCServer.Validate(), AddPrefixToFieldError("delegatedOIDCServer.")),
	)
}
