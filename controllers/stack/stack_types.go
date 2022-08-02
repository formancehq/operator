package stack

import (
	"context"
	"github.com/numary/formance-operator/apis/stack/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StackReconciler reconciles a Stack object
type StackReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type Config struct {
	Context     context.Context
	Request     ctrl.Request
	Stack       v1beta1.Stack
	Annotations map[string]string
	Labels      map[string]string
}

type ServiceConfig struct {
	Ports    []corev1.ServicePort
	Selector map[string]string
}
