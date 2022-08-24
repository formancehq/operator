package sharedtypes

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func ImagePullPolicy(image string) corev1.PullPolicy {
	imagePullPolicy := corev1.PullIfNotPresent
	if strings.HasSuffix(image, ":latest") {
		imagePullPolicy = corev1.PullAlways
	}
	return imagePullPolicy
}
