package v1beta2

import (
	authcomponentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	"github.com/numary/operator/pkg/apis/v1beta1"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type LedgerSpec struct {
	apisv1beta1.Scalable `json:",inline"`
	Postgres             apisv1beta1.PostgresConfig `json:"postgres"`
	// +optional
	LockingStrategy authcomponentsv1beta1.LockingStrategy `json:"locking"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}

func (in *LedgerSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	ret := typeutils.Map(in.Postgres.Validate(), v1beta1.AddPrefixToFieldError("postgres"))
	ret = append(ret, typeutils.Map(in.LockingStrategy.Validate(), v1beta1.AddPrefixToFieldError("locking"))...)
	return ret
}
