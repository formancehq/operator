package sharedtypes

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:generate=false
type Object interface {
	client.Object
	GetConditions() *Conditions
	IsDirty(t Object) bool
}
