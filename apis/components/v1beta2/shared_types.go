package v1beta2

import (
	pkgapisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

type PostgresConfigCreateDatabase struct {
	pkgapisv1beta2.PostgresConfigWithDatabase `json:",inline"`
	CreateDatabase                            bool `json:"createDatabase"`
}

type CollectorConfig struct {
	pkgapisv1beta2.KafkaConfig `json:",inline"`
	Topic                      string `json:"topic"`
}

func (c CollectorConfig) Env(prefix string) []corev1.EnvVar {
	ret := c.KafkaConfig.Env(prefix)
	return append(ret, pkgapisv1beta2.EnvWithPrefix(prefix, "PUBLISHER_TOPIC_MAPPING", "*:"+c.Topic))
}
