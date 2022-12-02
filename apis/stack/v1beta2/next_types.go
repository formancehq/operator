package v1beta2

import (
	"github.com/numary/operator/pkg/apis/v1beta1"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/typeutils"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// +kubebuilder:object:generate=true
type NextSpec struct {
	apisv1beta1.Scalable `json:",inline"`
	Postgres             apisv1beta1.PostgresConfig `json:"postgres"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}

func (l NextSpec) DatabaseSpec() apisv1beta1.PostgresConfigWithDatabase {
	return apisv1beta1.PostgresConfigWithDatabase{
		PostgresConfig: l.Postgres,
		Database:       "",
	}
}

func (in *NextSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	ret := typeutils.Map(in.Postgres.Validate(), v1beta1.AddPrefixToFieldError("postgres"))
	return ret
}
