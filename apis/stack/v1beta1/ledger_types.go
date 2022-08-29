package v1beta1

import (
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
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
	LockingStrategy authcomponentsv1beta1.LockingStrategy `json:"locking"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}

func (in *LedgerSpec) Validate() field.ErrorList {
	ret := Map(in.Postgres.Validate(), AddPrefixToFieldError("postgres"))
	ret = append(ret, Map(in.LockingStrategy.Validate(), AddPrefixToFieldError("locking"))...)
	return ret
}
