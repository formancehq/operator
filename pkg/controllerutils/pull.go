package controllerutils

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func ImagePullPolicy(o interface {
	GetImage() string
}) corev1.PullPolicy {
	image := o.GetImage()
	imagePullPolicy := corev1.PullIfNotPresent
	if strings.HasSuffix(image, ":latest") {
		imagePullPolicy = corev1.PullAlways
	}
	return imagePullPolicy
}
