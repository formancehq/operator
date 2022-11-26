// +kubebuilder:object:generate=true
package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type ConfigSource struct {
	// Selects a key of a ConfigMap.
	// +optional
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty" protobuf:"bytes,3,opt,name=configMapKeyRef"`
	// Selects a key of a secret in the pod's namespace
	// +optional
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty" protobuf:"bytes,4,opt,name=secretKeyRef"`
}

func (c *ConfigSource) Env() *corev1.EnvVarSource {
	return &corev1.EnvVarSource{
		ConfigMapKeyRef: c.ConfigMapKeyRef,
		SecretKeyRef:    c.SecretKeyRef,
	}
}

type PostgresConfig struct {
	// +optional
	Port int `json:"port"`
	// +optional
	PortFrom *ConfigSource `json:"portFrom"`
	// +optional
	Host string `json:"host"`
	// +optional
	HostFrom *ConfigSource `json:"hostFrom"`
	// +optional
	Username string `json:"username"`
	// +optional
	UsernameFrom *ConfigSource `json:"usernameFrom"`
	// +optional
	Password string `json:"password"`
	// +optional
	PasswordFrom *ConfigSource `json:"passwordFrom"`
}

type PostgresConfigWithDatabase struct {
	PostgresConfig `json:",inline"`
	// +optional
	Database string `json:"database"`
	// +optional
	DatabaseFrom *ConfigSource `json:"databaseFrom"`
}

func (c *PostgresConfigWithDatabase) Env(prefix string) []corev1.EnvVar {
	ret := make([]corev1.EnvVar, 0)
	ret = append(ret, SelectRequiredConfigValueOrReference("POSTGRES_DATABASE", prefix,
		c.Database, c.DatabaseFrom))
	return append(ret, c.PostgresConfig.Env(prefix)...)
}

func (c *PostgresConfigWithDatabase) Validate() field.ErrorList {
	ret := field.ErrorList{}
	ret = append(ret, c.PostgresConfig.Validate()...)
	return append(ret, ValidateRequiredConfigValueOrReference("database", c.Database, c.DatabaseFrom)...)
}

func (c *PostgresConfig) Validate() field.ErrorList {
	ret := field.ErrorList{}
	ret = append(ret, ValidateRequiredConfigValueOrReference("host", c.Host, c.HostFrom)...)
	ret = append(ret, ValidateRequiredConfigValueOrReference("port", c.Port, c.PortFrom)...)
	ret = append(ret, ValidateRequiredConfigValueOrReferenceOnly("username", c.Username, c.UsernameFrom)...)

	if c.Username != "" || c.UsernameFrom != nil {
		ret = append(ret, ValidateRequiredConfigValueOrReference("password", c.Password, c.PasswordFrom)...)
	}
	return ret
}

func (c *PostgresConfig) Env(prefix string) []corev1.EnvVar {

	ret := make([]corev1.EnvVar, 0)
	ret = append(ret, SelectRequiredConfigValueOrReference("POSTGRES_HOST", prefix, c.Host, c.HostFrom))
	ret = append(ret, SelectRequiredConfigValueOrReference("POSTGRES_PORT", prefix, c.Port, c.PortFrom))

	if c.Username != "" || c.UsernameFrom != nil {
		ret = append(ret, SelectRequiredConfigValueOrReference("POSTGRES_USERNAME", prefix, c.Username, c.UsernameFrom))
		ret = append(ret, SelectRequiredConfigValueOrReference("POSTGRES_PASSWORD", prefix, c.Password, c.PasswordFrom))

		ret = append(ret, EnvWithPrefix(prefix, "POSTGRES_URI",
			ComputeEnvVar(prefix, "postgresql://%s:%s@%s:%s",
				"POSTGRES_USERNAME",
				"POSTGRES_PASSWORD",
				"POSTGRES_HOST",
				"POSTGRES_PORT",
			),
		))
	} else {
		ret = append(ret, EnvWithPrefix(prefix, "POSTGRES_URI",
			ComputeEnvVar(prefix, "postgresql://%s:%s", "POSTGRES_HOST", "POSTGRES_PORT"),
		))
	}
	ret = append(ret, EnvWithPrefix(prefix, "POSTGRES_DATABASE_URI",
		ComputeEnvVar(prefix, "%s/%s", "POSTGRES_URI", "POSTGRES_DATABASE"),
	))

	return ret
}
