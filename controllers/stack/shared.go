package stack

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/numary/formance-operator/apis/stack/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *StackReconciler) reconcileService(ctx context.Context, actualStack *v1beta1.Stack, logger logr.Logger) (ctrl.Result, error) {
	logger.Info("Reconciling Service")

	deploy := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: actualStack.Name, Namespace: actualStack.Spec.Namespace}}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Spec.Ports = []corev1.ServicePort{{Port: 9090}}
		deploy.Spec.Selector = nil
		return nil
	})

	if err != nil {
		logger.Error(err, "Service reconcile failed")
		return ctrl.Result{}, err
	}

	logger.Info("Service ready")
	return ctrl.Result{}, nil
}
