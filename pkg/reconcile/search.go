package reconcile

import (
	"context"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NewSearchReconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx, "Search", req.NamespacedName)
	logger.Info("Starting Search reconciliation")

	return ctrl.Result{}, nil
}
