package reconcile

import (
	"context"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NewLedgerReconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx, "Ledger", req.NamespacedName)
	logger.Info("Starting Ledger reconciliation")

	return ctrl.Result{}, nil
}
