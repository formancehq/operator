package sharedtypes

import (
	"fmt"
	"strings"

	"github.com/numary/formance-operator/pkg/envutil"
	corev1 "k8s.io/api/core/v1"
)

type KafkaSASLConfig struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Mechanism    string `json:"mechanism"`
	ScramSHASize string `json:"scramSHASize"`
}

type KafkaConfig struct {
	Brokers []string         `json:"brokers"`
	TLS     bool             `json:"tls"`
	SASL    *KafkaSASLConfig `json:"sasl"`
}

type CollectorConfigSpec struct {
	// +required
	Kind string `json:"kind,omitempty"`
	// +optional
	KafkaConfig *KafkaConfig `json:"kafka"`
	// +optional
	TopicMapping map[string]string `json:"topicMapping"`
}

func (s *CollectorConfigSpec) Env(prefix string) []corev1.EnvVar {

	ret := make([]corev1.EnvVar, 0)
	switch s.Kind {
	case "http":
		ret = append(ret, envutil.EnvWithPrefix(prefix, "PUBLISHER_HTTP_ENABLED", "true"))
	case "kafka":
		ret = append(ret,
			envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_ENABLED", "true"),
			envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_BROKER", strings.Join(s.KafkaConfig.Brokers, ",")),
		)
		if s.KafkaConfig.SASL != nil {
			ret = append(ret,
				envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_ENABLED", "true"),
				envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_USERNAME", s.KafkaConfig.SASL.Username),
				envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_PASSWORD", s.KafkaConfig.SASL.Password),
				envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_MECHANISM", s.KafkaConfig.SASL.Mechanism),
				envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_SASL_SCRAM_SHA_SIZE", s.KafkaConfig.SASL.ScramSHASize),
			)
		}
		if s.KafkaConfig.TLS {
			ret = append(ret, envutil.EnvWithPrefix(prefix, "PUBLISHER_KAFKA_TLS_ENABLED", "true"))
		}
	}

	topicMapping := ""
	for k, v := range s.TopicMapping {
		topicMapping += fmt.Sprintf("%s:%s ", k, v)
	}
	ret = append(ret, envutil.EnvWithPrefix(prefix, "PUBLISHER_TOPIC_MAPPING", topicMapping))

	return ret
}
