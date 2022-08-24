package sharedtypes

import (
	"strings"

	"github.com/numary/formance-operator/internal/envutil"
	corev1 "k8s.io/api/core/v1"
)

type KafkaSASLConfig struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Mechanism    string `json:"mechanism"`
	ScramSHASize string `json:"scramSHASize"`
}

type KafkaConfig struct {
	Brokers []string `json:"brokers"`
	// +optional
	TLS bool `json:"tls"`
	// +optional
	SASL *KafkaSASLConfig `json:"sasl,omitempty"`
}

func (s *KafkaConfig) Env(prefix string) []corev1.EnvVar {

	ret := make([]corev1.EnvVar, 0)
	ret = append(ret,
		envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_ENABLED", "true"),
		envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_BROKER", strings.Join(s.Brokers, ",")),
	)
	if s.SASL != nil {
		ret = append(ret,
			envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_ENABLED", "true"),
			envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_USERNAME", s.SASL.Username),
			envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_PASSWORD", s.SASL.Password),
			envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_MECHANISM", s.SASL.Mechanism),
			envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_SCRAM_SHA_SIZE", s.SASL.ScramSHASize),
		)
	}
	if s.TLS {
		ret = append(ret, envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_TLS_ENABLED", "true"))
	}

	return ret
}
