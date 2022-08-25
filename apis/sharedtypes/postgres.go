// +kubebuilder:object:generate=true
package sharedtypes

import (
	"fmt"

	"github.com/numary/formance-operator/internal/envutil"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
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
	if c.Database != "" {
		ret = append(ret, envutil.EnvWithPrefix(prefix, "POSTGRES_DATABASE", c.Database))
	} else {
		ret = append(ret, envutil.EnvFromWithPrefix(prefix, "POSTGRES_DATABASE", c.DatabaseFrom.Env()))
	}
	return append(ret, c.PostgresConfig.Env(prefix)...)
}

func (c *PostgresConfigWithDatabase) Validate() error {
	if err := c.PostgresConfig.Validate(); err != nil {
		return err
	}
	if err := validate(c.Database, c.DatabaseFrom); err != nil {
		return errors.Wrap(err, "validation 'database' config")
	}
	return nil
}

func validateOneOf[T comparable](v T, source *ConfigSource) bool {
	var zeroValue T
	return v == zeroValue || source == nil
}

func validate[T comparable](v T, source *ConfigSource) error {
	var zeroValue T
	if v == zeroValue && source == nil {
		return errors.New("missing config")
	}
	if !validateOneOf(v, source) {
		return errors.New("invalid config")
	}
	return nil
}

func (c *PostgresConfig) Validate() error {
	if err := validate(c.Host, c.HostFrom); err != nil {
		return errors.Wrap(err, "validation 'host' config")
	}
	if err := validate(c.Port, c.PortFrom); err != nil {
		return errors.Wrap(err, "validation 'port' config")
	}
	if !validateOneOf(c.Username, c.UsernameFrom) {
		return fmt.Errorf("validation 'username' config")
	}

	if c.Username != "" || c.UsernameFrom != nil {
		if err := validate(c.Password, c.PasswordFrom); err != nil {
			return errors.New("missing 'password' config as 'username' is defined")
		}
	}
	return nil
}

func (c *PostgresConfig) Env(prefix string) []corev1.EnvVar {

	ret := []corev1.EnvVar{}
	if c.Host != "" {
		ret = append(ret, envutil.EnvWithPrefix(prefix, "POSTGRES_HOST", c.Host))
	} else {
		ret = append(ret, envutil.EnvFromWithPrefix(prefix, "POSTGRES_HOST", &corev1.EnvVarSource{
			ConfigMapKeyRef: c.HostFrom.ConfigMapKeyRef,
			SecretKeyRef:    c.HostFrom.SecretKeyRef,
		}))
	}
	if c.Port != 0 {
		ret = append(ret, envutil.EnvWithPrefix(prefix, "POSTGRES_PORT", fmt.Sprintf("%d", c.Port)))
	} else {
		ret = append(ret, envutil.EnvFromWithPrefix(prefix, "POSTGRES_PORT", c.PortFrom.Env()))
	}
	if c.Username != "" || c.UsernameFrom != nil {
		if c.Username != "" {
			ret = append(ret, envutil.EnvWithPrefix(prefix, "POSTGRES_USERNAME", c.Username))
		}
		if c.UsernameFrom != nil {
			ret = append(ret, envutil.EnvFromWithPrefix(prefix, "POSTGRES_USERNAME", c.UsernameFrom.Env()))
		}
		if c.Password != "" {
			ret = append(ret, envutil.EnvWithPrefix(prefix, "POSTGRES_PASSWORD", c.Password))
		}
		if c.PasswordFrom != nil {
			ret = append(ret, envutil.EnvFromWithPrefix(prefix, "POSTGRES_PASSWORD", c.PasswordFrom.Env()))
		}
		ret = append(ret, envutil.EnvWithPrefix(prefix, "POSTGRES_URI",
			fmt.Sprintf("postgresql://%s:%s@%s:%s",
				"$("+prefix+"POSTGRES_USERNAME)",
				"$("+prefix+"POSTGRES_PASSWORD)",
				"$("+prefix+"POSTGRES_HOST)",
				"$("+prefix+"POSTGRES_PORT)",
			)))
	} else {
		ret = append(ret, envutil.EnvWithPrefix(prefix, "POSTGRES_URI",
			fmt.Sprintf("postgresql://%s:%s",
				"$("+prefix+"POSTGRES_HOST)",
				"$("+prefix+"POSTGRES_PORT)",
			)))
	}
	ret = append(ret, envutil.EnvWithPrefix(prefix, "POSTGRES_DATABASE_URI",
		fmt.Sprintf("%s/%s",
			"$("+prefix+"POSTGRES_URI)",
			"$("+prefix+"POSTGRES_DATABASE)",
		),
	))

	return ret
}
