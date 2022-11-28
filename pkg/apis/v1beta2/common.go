package v1beta2

import (
	"fmt"

	"github.com/numary/operator/pkg/apis/v1beta1"
	v1 "k8s.io/api/core/v1"
)

type DevProperties struct {
	// +optional
	Debug bool `json:"debug"`
	// +optional
	Dev bool `json:"dev"`
}

func (d DevProperties) Env() []v1.EnvVar {
	return d.EnvWithPrefix("")
}

func (d DevProperties) EnvWithPrefix(prefix string) []v1.EnvVar {
	return []v1.EnvVar{
		v1beta1.EnvWithPrefix(prefix, "DEBUG", fmt.Sprintf("%v", d.Debug)),
		v1beta1.EnvWithPrefix(prefix, "DEV", fmt.Sprintf("%v", d.Dev)),
	}
}

type CommonServiceProperties struct {
	DevProperties `json:",inline"`
	// +optional
	// +kubebuilder:default:="latest"
	Version string `json:"version,omitempty"`
}

func (p CommonServiceProperties) GetVersion() string {
	return p.Version
}
