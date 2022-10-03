package v1beta1

import (
	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/internal/collectionutil"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type MongoDBConfig struct {
	// +optional
	UseSrv bool `json:"useSrv"`
	// +optional
	Host string `json:"host,omitempty"`
	// +optional
	HostFrom *ConfigSource `json:"hostFrom,omitempty"`
	// +optional
	Port uint16 `json:"port,omitempty"`
	// +optional
	PortFrom *ConfigSource `json:"portFrom,omitempty"`
	// +optional
	Username string `json:"username,omitempty"`
	// +optional
	UsernameFrom *ConfigSource `json:"usernameFrom,omitempty"`
	// +optional
	Password string `json:"password,omitempty"`
	// +optional
	PasswordFrom *ConfigSource `json:"passwordFrom,omitempty"`
}

func (in *MongoDBConfig) Validate() field.ErrorList {
	return MergeAll(
		ValidateRequiredConfigValueOrReference("host", in.Host, in.HostFrom),
		ValidateRequiredConfigValueOrReference("port", in.Port, in.PortFrom),
		ValidateRequiredConfigValueOrReference("username", in.Username, in.UsernameFrom),
		ValidateRequiredConfigValueOrReference("password", in.Password, in.PasswordFrom),
	)
}

// +kubebuilder:object:generate=true
type PaymentsSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	MongoDB MongoDBConfig  `json:"mongoDB"`
}

func (in *PaymentsSpec) Validate() field.ErrorList {
	if in == nil {
		return nil
	}
	return Map(in.MongoDB.Validate(), AddPrefixToFieldError("mongoDB."))
}
