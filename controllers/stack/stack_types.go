package stack

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StackReconciler reconciles a Stack object
type StackReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}
