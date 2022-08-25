package v1beta1

import (
	"github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
)

type AuthSpec struct {
	ImageHolder    `json:",inline"`
	PostgresConfig PostgresConfig `json:"postgres"`
	// +optional
	SigningKey          string                                   `json:"signingKey"`
	DelegatedOIDCServer v1beta1.DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	// +required
	Host string `json:"host,omitempty"`
	// +optional
	Scheme string `json:"scheme,omitempty"`
	// +optional
	Debug bool `json:"debug"`
}

func (in *AuthSpec) GetScheme() string {
	if in.Scheme != "" {
		return in.Scheme
	}
	return "https"
}