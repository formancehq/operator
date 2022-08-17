// +kubebuilder:object:generate=true
package sharedtypes

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IngressSpec struct {
	Path string `json:"path"`
	Host string `json:"host"`
	// +optional
	Annotations map[string]string `json:"annotations"`
}

const (
	ConditionTypeIngressReady = "IngressReady"
)

func SetIngressReady(object Object, msg ...string) {
	SetCondition(object, ConditionTypeIngressReady, metav1.ConditionTrue, msg...)
}

func SetIngressError(object Object, msg ...string) {
	SetCondition(object, ConditionTypeIngressReady, metav1.ConditionFalse, msg...)
}

func RemoveIngressCondition(object Object) {
	object.GetConditions().Remove(ConditionTypeIngressReady)
}
