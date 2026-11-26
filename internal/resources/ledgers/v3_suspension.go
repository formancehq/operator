package ledgers

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

// disableLedger preserves every Ledger v3 identity and TLS resource. Ledger v2
// workloads retain their historical cleanup; a concurrent v3 preview Cluster
// is suspended instead of deleted.
func disableLedger(ctx core.Context, stack *v1beta1.Stack, options *core.ReconcilerOptions[*v1beta1.Ledger], ledger *v1beta1.Ledger) error {
	if !ledgerV3ClusterAvailable {
		version, err := core.GetModuleVersion(ctx, stack, ledger)
		if err != nil {
			return err
		}
		if isLedgerV3(version) {
			return core.NewPendingError().WithMessage("Ledger v3 Cluster CRD is unavailable; refusing to delete durable Ledger resources")
		}
		return core.RemoveAllModuleOwnedObjects(ctx, ledger, options.Owns)
	}
	cluster, exists, err := getV3Cluster(ctx, stack)
	if err != nil {
		return err
	}
	if !exists {
		return core.RemoveAllModuleOwnedObjects(ctx, ledger, options.Owns)
	}
	if !cluster.GetDeletionTimestamp().IsZero() {
		return core.NewPendingError().WithMessage("Ledger v3 Cluster is deleting; reversible suspension is unavailable")
	}
	if err := setV3ClusterSuspend(ctx, cluster, true); err != nil {
		return err
	}
	// Keep the Cluster, Issuer and Certificate so the original Raft identity,
	// retained volumes, and TLS trust root survive the disabled interval.
	phase, _, err := unstructured.NestedString(cluster.Object, "status", "phase")
	if err != nil {
		return err
	}
	observed, _, err := unstructured.NestedInt64(cluster.Object, "status", "observedGeneration")
	if err != nil {
		return err
	}
	if phase != "Suspended" || cluster.GetGeneration() != observed {
		return core.NewPendingError().WithMessage("waiting for Ledger v3 Cluster to suspend")
	}
	var removable []client.Object
	for object := range options.Owns {
		if _, durable := object.(v1beta1.Resource); durable {
			continue
		}
		gvk := object.GetObjectKind().GroupVersionKind()
		if gvk == ledgerV3ClusterGVK || gvk == ledgerV3IssuerGVK || gvk == ledgerV3CertificateGVK {
			continue
		}
		removable = append(removable, object)
	}
	if len(removable) == 0 {
		return nil
	}
	return core.DeleteOwnedObjects(ctx, ledger, removable...)
}

func setV3ClusterSuspend(ctx core.Context, cluster *unstructured.Unstructured, suspend bool) error {
	current, _, err := unstructured.NestedBool(cluster.Object, "spec", "suspend")
	if err != nil {
		return fmt.Errorf("reading Ledger v3 suspension request: %w", err)
	}
	if current == suspend {
		return nil
	}
	before := cluster.DeepCopy()
	if err := unstructured.SetNestedField(cluster.Object, suspend, "spec", "suspend"); err != nil {
		return err
	}
	return ctx.GetClient().Patch(ctx, cluster, client.MergeFrom(before))
}

// resumeV3Cluster keeps the old image, replica count and template pinned
// until the Ledger operator has started and verified all retained members.
func resumeV3Cluster(ctx core.Context, stack *v1beta1.Stack, tlsReady bool) (bool, error) {
	if !ledgerV3ClusterAvailable {
		return false, nil
	}
	cluster, exists, err := getV3Cluster(ctx, stack)
	if err != nil || !exists {
		return false, err
	}
	phase, _, err := unstructured.NestedString(cluster.Object, "status", "suspension", "phase")
	if err != nil {
		return false, err
	}
	suspended, _, err := unstructured.NestedBool(cluster.Object, "spec", "suspend")
	if err != nil {
		return false, err
	}
	if !suspended && (phase == "" || phase == "Running") {
		return false, nil
	}
	if !tlsReady {
		return true, core.NewPendingError().WithMessage("waiting for Ledger v3 TLS before restarting retained members")
	}
	if err := setV3ClusterSuspend(ctx, cluster, false); err != nil {
		return true, err
	}
	return true, core.NewPendingError().WithMessage("waiting for Ledger v3 Cluster to resume")
}
