package sharedtypes

import (
	"fmt"

	"github.com/numary/formance-operator/pkg/envutil"
	corev1 "k8s.io/api/core/v1"
)

type CollectorConfigSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// +required
	Kind *string `json:"kind,omitempty"`

	// +optional
	TopicMapping map[string]string `json:"topicMapping"`
}

func (s *CollectorConfigSpec) Env(prefix string) []corev1.EnvVar {

	ret := []corev1.EnvVar{}
	if s == nil || !s.Enabled || s.Kind == nil {
		return ret
	}
	switch *s.Kind {
	case "http":
		ret = append(ret, envutil.EnvWithPrefix(prefix, "PUBLISHER_HTTP_ENABLED", "true"))
		topicMapping := ""
		for k, v := range s.TopicMapping {
			topicMapping += fmt.Sprintf("%s:%s ", k, v)
		}
		ret = append(ret, envutil.EnvWithPrefix(prefix, "PUBLISHER_TOPIC_MAPPING", topicMapping))
	}
	return ret
}
