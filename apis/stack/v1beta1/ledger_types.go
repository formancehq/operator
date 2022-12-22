package v1beta1

import (
	authcomponentsv1beta2 "github.com/formancehq/operator/apis/components/v1beta2"
	. "github.com/formancehq/operator/pkg/apis/v1beta2"
	. "github.com/formancehq/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type LedgerSpec struct {
	ImageHolder `json:",inline"`
	Scalable    `json:",inline"`
	// +optional
	Postgres PostgresConfig `json:"postgres"`
	// +optional
	LockingStrategy authcomponentsv1beta2.LockingStrategy `json:"locking"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}

func (in *LedgerSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	ret := Map(in.Postgres.Validate(), AddPrefixToFieldError("postgres"))
	ret = append(ret, Map(in.LockingStrategy.Validate(), AddPrefixToFieldError("locking"))...)
	return ret
}
