package ledgers

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestV3TransitionRestorationEvidence(t *testing.T) {
	for _, scenario := range []string{"remembered-primary", "preview-only", "missing", "configured-only", "conflicting-config", "conflicting-primary-marker", "stale-marker", "replaced-ledger"} {
		t.Run(scenario, func(t *testing.T) {
			stack, ledger, _ := identityTestObjects(t)
			ledger.Annotations = map[string]string{ledgerV3IdentityAnnotation: identityTestUUID, ledgerV3PrimaryIdentityAnnotation: identityTestUUID}
			config := ledgerConfiguration("default", 0, "*")
			switch scenario {
			case "preview-only":
				delete(ledger.Annotations, ledgerV3PrimaryIdentityAnnotation)
			case "missing":
				ledger.Annotations = nil
			case "configured-only":
				ledger.Annotations = nil
				config.Spec.Cluster.ClusterID = identityTestUUID
			case "conflicting-primary-marker":
				ledger.Annotations[ledgerV3PrimaryIdentityAnnotation] = "different"
			case "conflicting-config":
				config.Spec.Cluster.ClusterID = "different"
			}
			payments := &v1beta1.Payments{ObjectMeta: metav1.ObjectMeta{Name: "payments"}, Spec: v1beta1.PaymentsSpec{StackDependency: v1beta1.StackDependency{Stack: stack.Name}}}
			ctx := identityTestContext(t, ledger, config, payments)
			staleLedger := ledger.DeepCopy()
			if scenario == "stale-marker" || scenario == "replaced-ledger" {
				current := &v1beta1.Ledger{}
				require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), current))
				if scenario == "stale-marker" {
					current.Annotations = nil
				} else {
					current.UID = "replacement"
				}
				require.NoError(t, ctx.GetClient().Update(ctx, current))
			}
			err := blockV3TransitionWithIncompatibleModules(ctx, stack, staleLedger)
			if scenario == "remembered-primary" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if scenario == "preview-only" || scenario == "missing" || scenario == "configured-only" || scenario == "stale-marker" {
				require.ErrorContains(t, err, "disable incompatible modules before switching Ledger to v3: Payments")
			}
			if scenario == "conflicting-config" {
				require.ErrorContains(t, err, "clusterID conflict")
			}
			if scenario == "conflicting-primary-marker" {
				require.ErrorContains(t, err, "primary Ledger v3 identity marker disagrees")
			}
			if scenario == "replaced-ledger" {
				require.ErrorContains(t, err, "Ledger was replaced")
			}
		})
	}
}

func TestV3TransitionRememberedIdentityKeepsLegacyStorageGuard(t *testing.T) {
	old := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = old })
	stack, ledger, _ := identityTestObjects(t)
	ledger.Annotations = map[string]string{ledgerV3IdentityAnnotation: identityTestUUID, ledgerV3PrimaryIdentityAnnotation: identityTestUUID}
	database := &v1beta1.Database{ObjectMeta: metav1.ObjectMeta{Name: stack.Name + "-ledger"}}
	ctx := identityTestContext(t, ledger, database)
	require.NoError(t, blockV3TransitionWithIncompatibleModules(ctx, stack, ledger))
	err := reconcileV3(ctx, stack, ledger, "v3.0.0-rc.1")
	require.ErrorContains(t, err, "migration required")
}

func TestLedgerV3PrimaryMarkerRequiresObservedPrimary(t *testing.T) {
	for _, preview := range []bool{false, true} {
		name := "primary"
		if preview {
			name = "preview"
		}
		t.Run(name, func(t *testing.T) {
			stack, ledger, cluster := identityTestObjects(t)
			// Upgrade an already remembered identity: a preview must not gain the
			// primary restoration marker, even though both share the same child name.
			ledger.Annotations = map[string]string{ledgerV3IdentityAnnotation: identityTestUUID}
			if preview {
				cluster.SetLabels(map[string]string{ledgerV3PreviewLabel: "true"})
			}
			ctx := identityTestContext(t, ledger, cluster)
			id, err := rememberLedgerV3Identity(ctx, stack, ledger)
			require.NoError(t, err)
			require.Equal(t, identityTestUUID, id)
			saved := &v1beta1.Ledger{}
			require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), saved))
			require.Equal(t, identityTestUUID, saved.Annotations[ledgerV3IdentityAnnotation])
			if preview {
				require.Empty(t, saved.Annotations[ledgerV3PrimaryIdentityAnnotation])
			} else {
				require.Equal(t, identityTestUUID, saved.Annotations[ledgerV3PrimaryIdentityAnnotation])
			}
		})
	}
}

func TestLedgerV3ConfiguredIDDoesNotProveObservedPrimaryIdentity(t *testing.T) {
	stack, ledger, cluster := identityTestObjects(t)
	cluster.Object["spec"] = map[string]any{}
	config := ledgerConfiguration("default", 0, "*")
	config.Spec.Cluster.ClusterID = identityTestUUID
	ctx := identityTestContext(t, ledger, cluster, config)
	_, err := rememberLedgerV3Identity(ctx, stack, ledger)
	require.NoError(t, err)
	saved := &v1beta1.Ledger{}
	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), saved))
	require.Empty(t, saved.Annotations[ledgerV3PrimaryIdentityAnnotation])
}
