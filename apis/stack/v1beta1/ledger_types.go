package v1beta1

import (
	"fmt"

	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal/collectionutil"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type LedgerSpec struct {
	ImageHolder `json:",inline"`
	Scalable    `json:",inline"`
	// +optional
	Debug    bool           `json:"debug,omitempty"`
	Postgres PostgresConfig `json:"postgres"`
	// +optional
	Redis *authcomponentsv1beta1.RedisConfig `json:"redis"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}

func (in *LedgerSpec) Validate() field.ErrorList {
	return collectionutil.Map(in.Postgres.Validate(), func(t1 *field.Error) *field.Error {
		t1.Field = fmt.Sprintf("postgres.%s", t1.Field)
		return t1
	})
}
