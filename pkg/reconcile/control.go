package reconcile

import (
	"context"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NewControlReconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx, "Control", req.NamespacedName)
	logger.Info("Starting Control reconciliation")

	return ctrl.Result{}, nil
}
