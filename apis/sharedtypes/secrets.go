package sharedtypes

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeSecretReady = "SecretReady"
)

func SetSecretReady(object Object, msg ...string) {
	SetCondition(object, ConditionTypeSecretReady, metav1.ConditionTrue, msg...)
}

func SetSecretError(object Object, msg ...string) {
	SetCondition(object, ConditionTypeSecretReady, metav1.ConditionFalse, msg...)
}
