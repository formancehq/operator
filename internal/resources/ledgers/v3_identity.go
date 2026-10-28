package ledgers

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

const ledgerV3IdentityAnnotation = "formance.com/ledger-v3-cluster-id"

// Compose the standard module lifecycle locally so identity is durable before
// its disable path deletes the child Cluster. Keep the standard Stack and
// Versions watches as well as all Ledger-specific options.
func withLedgerModuleReconciler(opts ...core.ReconcilerOption[*v1beta1.Ledger]) core.Initializer {
	var options *core.ReconcilerOptions[*v1beta1.Ledger]
	opts = append(opts, core.WithWatchVersions[*v1beta1.Ledger](core.NoRequirements()),
		func(o *core.ReconcilerOptions[*v1beta1.Ledger]) { options = o })
	return core.WithStackDependencyReconciler(func(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger) error {
		return reconcileLedgerModule(ctx, stack, options, ledger)
	}, opts...)
}

func reconcileLedgerModule(ctx core.Context, stack *v1beta1.Stack, options *core.ReconcilerOptions[*v1beta1.Ledger], ledger *v1beta1.Ledger) error {
	if ledgerV3ClusterAvailable {
		if _, err := rememberLedgerV3Identity(ctx, stack, ledger); err != nil {
			return err
		}
	}
	return core.ForModule(core.NoRequirements(), func(ctx core.Context, stack *v1beta1.Stack, _ *core.ReconcilerOptions[*v1beta1.Ledger], ledger *v1beta1.Ledger, version string) error {
		return Reconcile(ctx, stack, ledger, version)
	})(ctx, stack, options, ledger)
}

// An existing child is authoritative, but conflicting explicit or previously
// committed identities are never silently replaced: its PVCs may still exist.
func ledgerV3Identity(configured, remembered string, cluster *unstructured.Unstructured) (string, error) {
	current, _, err := unstructured.NestedString(cluster.Object, "spec", "clusterID")
	if err != nil {
		return "", fmt.Errorf("reading Ledger v3 Cluster identity: %w", err)
	}
	identity := ""
	for _, id := range []string{current, configured, remembered} {
		if id == "" {
			continue
		}
		if identity != "" && identity != id {
			return "", fmt.Errorf("Ledger v3 clusterID conflict between child, LedgerConfiguration and remembered identity; restore matching identity before reconciling")
		}
		identity = id
	}
	return identity, nil
}

func rememberLedgerV3Identity(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger) (string, error) {
	// Read lifecycle state directly: a cached missing child must not allow cleanup
	// or recreation to forget the identity already committed by its operator.
	currentLedger := &v1beta1.Ledger{}
	if err := ctx.GetAPIReader().Get(ctx, client.ObjectKeyFromObject(ledger), currentLedger); err != nil {
		return "", err
	}
	if currentLedger.UID != ledger.UID {
		return "", fmt.Errorf("Ledger was replaced while preserving its v3 identity")
	}
	cluster := newV3Cluster()
	err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
	if err != nil && !apierrors.IsNotFound(err) {
		return "", err
	}
	exists := err == nil
	if exists && !metav1.IsControlledBy(cluster, currentLedger) {
		return "", fmt.Errorf("Ledger v3 Cluster is not controlled by this Ledger; refusing to copy its identity")
	}
	remembered := currentLedger.Annotations[ledgerV3IdentityAnnotation]
	if !exists && remembered == "" {
		return "", nil
	}
	base, err := ledgerV3BaseSpec(ctx, stack.Name)
	if err != nil {
		return "", err
	}
	identity, err := ledgerV3Identity(base.ClusterID, remembered, cluster)
	if err != nil {
		return "", err
	}
	if exists && (stack.Spec.Disabled || ledger.IsDisabled()) && base.ClusterID == "" {
		currentID, _, err := unstructured.NestedString(cluster.Object, "spec", "clusterID")
		if err != nil {
			return "", err
		}
		if currentID == "" {
			return "", core.NewPendingError().WithMessage("waiting for owned Ledger v3 Cluster clusterID before disable cleanup")
		}
	}
	// Do not alter a legacy Ledger with no v3 child. Configured identity is
	// applied by the v3 builder, then recorded once the child actually exists.
	if !exists || identity == "" || identity == remembered {
		return identity, nil
	}
	patch := client.MergeFromWithOptions(currentLedger.DeepCopy(), client.MergeFromWithOptimisticLock{})
	if currentLedger.Annotations == nil {
		currentLedger.Annotations = map[string]string{}
	}
	currentLedger.Annotations[ledgerV3IdentityAnnotation] = identity
	if err := ctx.GetClient().Patch(ctx, currentLedger, patch); err != nil {
		return "", fmt.Errorf("persisting Ledger v3 identity before lifecycle cleanup: %w", err)
	}
	ledger.Annotations = currentLedger.Annotations
	ledger.ResourceVersion = currentLedger.ResourceVersion
	return identity, nil
}
