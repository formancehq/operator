package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypePodReady = "PodReady"
)

func SetPodReady(object Object, msg ...string) {
	SetCondition(object, ConditionTypePodReady, metav1.ConditionTrue, msg...)
}

func SetPodError(object Object, msg ...string) {
	SetCondition(object, ConditionTypePodReady, metav1.ConditionFalse, msg...)
}

func RemovePodCondition(object Object) {
	object.GetConditions().Remove(ConditionTypePodReady)
}
