package controllerutils

import (
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeSecretReady = "SecretReady"
)

func SetSecretReady(object apisv1beta1.Object, msg ...string) {
	apisv1beta1.SetCondition(object, ConditionTypeSecretReady, metav1.ConditionTrue, msg...)
}

func SetSecretError(object apisv1beta1.Object, msg ...string) {
	apisv1beta1.SetCondition(object, ConditionTypeSecretReady, metav1.ConditionFalse, msg...)
}
