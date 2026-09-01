package ledgers

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

var ledgerV3IncompatibleModules = []struct {
	kind      string
	newModule func() client.Object
}{
	{kind: "MCP", newModule: func() client.Object { return &v1beta1.MCP{} }},
	{kind: "Orchestration", newModule: func() client.Object { return &v1beta1.Orchestration{} }},
	{kind: "Reconciliation", newModule: func() client.Object { return &v1beta1.Reconciliation{} }},
	{kind: "TransactionPlane", newModule: func() client.Object { return &v1beta1.TransactionPlane{} }},
	{kind: "Wallets", newModule: func() client.Object { return &v1beta1.Wallets{} }},
	{kind: "Webhooks", newModule: func() client.Object { return &v1beta1.Webhooks{} }},
}

// HasPrimaryV3Cluster reports whether the Stack has materialized its primary
// Ledger v3 Cluster. A migration preview does not change the compatibility of
// the legacy modules still using Ledger v2.
func HasPrimaryV3Cluster(ctx core.Context, stack *v1beta1.Stack) (bool, error) {
	cluster, exists, err := getV3Cluster(ctx, stack)
	if err != nil || !exists {
		return false, err
	}
	return !isLedgerV3Preview(cluster), nil
}

// CleanupLegacyModuleOnV3 creates the lifecycle handler used by legacy Ledger
// consumers. Desired-version changes alone must not stop working v2 services;
// cleanup starts only after the primary Ledger v3 Cluster actually exists.
func CleanupLegacyModuleOnV3[T v1beta1.Module](objects ...client.Object) core.UnsatisfiedRequirementsHandler[T] {
	return func(ctx core.Context, stack *v1beta1.Stack, module T) error {
		active, err := HasPrimaryV3Cluster(ctx, stack)
		if err != nil {
			return err
		}
		if !active {
			return nil
		}
		return core.DeleteOwnedObjects(ctx, module, objects...)
	}
}

func blockV3TransitionWithIncompatibleModules(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger) error {
	active, err := HasPrimaryV3Cluster(ctx, stack)
	if err != nil {
		return err
	}
	if active {
		return nil
	}

	installed := make([]string, 0, len(ledgerV3IncompatibleModules))
	for _, candidate := range ledgerV3IncompatibleModules {
		found, err := core.HasDependency(ctx, stack.Name, candidate.newModule())
		if err != nil {
			return err
		}
		if found {
			installed = append(installed, candidate.kind)
		}
	}
	if len(installed) == 0 {
		return nil
	}

	message := fmt.Sprintf(
		"disable incompatible modules before switching Ledger to v3: %s",
		strings.Join(installed, ", "),
	)
	setLedgerV3Condition(ledger, metav1.ConditionFalse, "IncompatibleModules", message)
	return core.NewPendingError().WithMessage("%s", message)
}
