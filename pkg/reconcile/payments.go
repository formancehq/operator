package reconcile

import (
	"context"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NewPaymentsReconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx, "Payments", req.NamespacedName)
	logger.Info("Starting Payments reconciliation")

	return ctrl.Result{}, nil
}
