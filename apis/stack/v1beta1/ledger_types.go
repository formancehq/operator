package v1beta1

import (
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
)

// +kubebuilder:object:generate=true
type LedgerSpec struct {
	// +optional
	Debug bool `json:"debug,omitempty"`
	// +optional
	Scaling  ScalingSpec    `json:"scaling,omitempty"`
	Postgres PostgresConfig `json:"postgres"`
	// +optional
	Redis *authcomponentsv1beta1.RedisConfig `json:"redis"`
	// +optional
	Image string `json:"image"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
}
