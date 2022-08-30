package v1beta1

import (
	"fmt"

	"github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type AuthSpec struct {
	ImageHolder `json:",inline"`
	Postgres    PostgresConfig `json:"postgres"`
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

func (in *AuthSpec) Validate() field.ErrorList {
	return Map(in.Postgres.Validate(), func(t1 *field.Error) *field.Error {
		t1.Field = fmt.Sprintf("postgres.%s", t1.Field)
		return t1
	})
}
