package ledgers

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

const identityTestUUID = "12345678-1234-4234-8234-123456789abc"

func identityTestContext(t *testing.T, objects ...client.Object) reindexTestContext {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))
	scheme.AddKnownTypeWithName(ledgerV3ClusterGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(ledgerV3ClusterGVK.GroupVersion().WithKind("ClusterList"), &unstructured.UnstructuredList{})
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithIndex(&v1beta1.LedgerConfiguration{}, "stack", func(o client.Object) []string { return o.(*v1beta1.LedgerConfiguration).GetStacks() }).
		WithIndex(&v1beta1.Settings{}, "stack", func(o client.Object) []string { return o.(*v1beta1.Settings).GetStacks() }).
		WithIndex(&v1beta1.Settings{}, "keylen", func(o client.Object) []string {
			return []string{strconv.Itoa(len(settings.SplitKeywordWithDot(o.(*v1beta1.Settings).Spec.Key)))}
		})
	for _, o := range []v1beta1.Dependent{&v1beta1.Auth{}, &v1beta1.MCP{}, &v1beta1.Orchestration{}, &v1beta1.Payments{}, &v1beta1.Reconciliation{}, &v1beta1.TransactionPlane{}, &v1beta1.Wallets{}, &v1beta1.Webhooks{}} {
		b = b.WithIndex(o, "stack", func(o client.Object) []string { return []string{o.(v1beta1.Dependent).GetStack()} })
	}
	return reindexTestContext{Context: t.Context(), client: b.WithObjects(objects...).Build()}
}

func identityTestObjects(t *testing.T) (*v1beta1.Stack, *v1beta1.Ledger, *unstructured.Unstructured) {
	t.Helper()
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: "stack-uid"}, Spec: v1beta1.StackSpec{Version: "v3.0.0-rc.1"}}
	ledger := reindexTestLedger()
	cluster := newV3Cluster()
	cluster.SetName(stack.Name)
	cluster.SetNamespace(stack.Name)
	cluster.SetUID("original-cluster")
	require.NoError(t, unstructured.SetNestedField(cluster.Object, identityTestUUID, "spec", "clusterID"))
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	require.NoError(t, controllerutil.SetControllerReference(ledger, cluster, scheme))
	return stack, ledger, cluster
}

func identityOptions() *core.ReconcilerOptions[*v1beta1.Ledger] {
	return &core.ReconcilerOptions[*v1beta1.Ledger]{Owns: map[client.Object][]builder.OwnsOption{newV3Cluster(): nil}}
}

func TestLedgerV3IdentityDisableReenable(t *testing.T) {
	old := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = old })
	for _, moduleDisabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("module-disabled-%t", moduleDisabled), func(t *testing.T) {
			stack, ledger, cluster := identityTestObjects(t)
			stack.Spec.Disabled = !moduleDisabled
			ledger.Spec.Disabled = moduleDisabled
			var objects []client.Object
			for _, volume := range []string{"wal", "data"} {
				for ordinal := 0; ordinal < 3; ordinal++ {
					objects = append(objects, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-ledger-%s-%d", volume, stack.Name, ordinal), Namespace: stack.Name, UID: types.UID(fmt.Sprintf("pvc-%s-%d", volume, ordinal))}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: fmt.Sprintf("pv-%s-%d", volume, ordinal)}})
				}
			}
			objects = append(objects, stack, ledger, cluster)
			ctx := identityTestContext(t, objects...)
			pvcDeletes := 0
			ctx.client = interceptor.NewClient(ctx.client.(client.WithWatch), interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.DeleteOption) error {
				if _, ok := o.(*corev1.PersistentVolumeClaim); ok {
					pvcDeletes++
				}
				return c.Delete(ctx, o, opts...)
			}})
			require.NoError(t, reconcileLedgerModule(ctx, stack, identityOptions(), ledger))
			require.True(t, apierrors.IsNotFound(ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), newV3Cluster())))
			saved := &v1beta1.Ledger{}
			require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), saved))
			require.Equal(t, identityTestUUID, saved.Annotations[ledgerV3IdentityAnnotation])
			stack.Spec.Disabled = false
			saved.Spec.Disabled = false
			require.NoError(t, ctx.GetClient().Update(ctx, saved))
			// Real module enable reaches the real builder even while TLS remains pending.
			err := reconcileLedgerModule(ctx, stack, identityOptions(), saved)
			require.ErrorContains(t, err, "TLS is not ready")
			recreated := newV3Cluster()
			require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), recreated))
			id, _, err := unstructured.NestedString(recreated.Object, "spec", "clusterID")
			require.NoError(t, err)
			require.Equal(t, identityTestUUID, id)
			require.Equal(t, cluster.GetName(), recreated.GetName())
			require.Equal(t, cluster.GetNamespace(), recreated.GetNamespace())
			require.Zero(t, pvcDeletes)
			for _, o := range objects[:6] {
				pvc := &corev1.PersistentVolumeClaim{}
				require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(o), pvc))
				require.Equal(t, o.GetUID(), pvc.UID)
				require.Equal(t, o.(*corev1.PersistentVolumeClaim).Spec, pvc.Spec)
			}
		})
	}
}

func TestLedgerV3IdentityFailureBlocksCleanup(t *testing.T) {
	old := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = old })
	for _, scenario := range []string{"patch-failure", "patch-conflict", "configured-conflict", "remembered-conflict", "foreign-child", "foreign-owner"} {
		t.Run(scenario, func(t *testing.T) {
			stack, ledger, cluster := identityTestObjects(t)
			stack.Spec.Disabled = true
			var extra []client.Object
			switch scenario {
			case "configured-conflict":
				config := ledgerConfiguration("default", 0, "*")
				config.Spec.Cluster.ClusterID = "different"
				extra = append(extra, config)
			case "remembered-conflict":
				ledger.Annotations = map[string]string{ledgerV3IdentityAnnotation: "different"}
			case "foreign-child":
				cluster.SetOwnerReferences(nil)
			case "foreign-owner":
				refs := cluster.GetOwnerReferences()
				refs[0].UID = "another-ledger"
				cluster.SetOwnerReferences(refs)
			}
			ctx := identityTestContext(t, append(extra, stack, ledger, cluster)...)
			if scenario == "patch-failure" || scenario == "patch-conflict" {
				ctx.client = interceptor.NewClient(ctx.client.(client.WithWatch), interceptor.Funcs{Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					if scenario == "patch-conflict" {
						return apierrors.NewConflict(schema.GroupResource{Group: "formance.com", Resource: "ledgers"}, ledger.Name, fmt.Errorf("stale version"))
					}
					return fmt.Errorf("write rejected")
				}})
			}
			require.Error(t, reconcileLedgerModule(ctx, stack, identityOptions(), ledger))
			require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), newV3Cluster()))
		})
	}
}

func TestLedgerV3IdentityMissingChildAndPreview(t *testing.T) {
	for _, preview := range []bool{false, true} {
		t.Run(fmt.Sprintf("preview-%t", preview), func(t *testing.T) {
			stack, ledger, _ := identityTestObjects(t)
			ledger.Annotations = map[string]string{ledgerV3IdentityAnnotation: identityTestUUID}
			ctx := identityTestContext(t, ledger)
			cluster, spec, err := createOrUpdateV3Cluster(ctx, stack, ledger, "v3.0.0-rc.1", preview, "")
			require.NoError(t, err)
			require.Equal(t, identityTestUUID, spec.ClusterID)
			id, _, err := unstructured.NestedString(cluster.Object, "spec", "clusterID")
			require.NoError(t, err)
			require.Equal(t, identityTestUUID, id)
		})
	}
}

func TestLedgerV3IdentityLegacyAbsentUnchanged(t *testing.T) {
	stack, ledger, _ := identityTestObjects(t)
	stack.Spec.Version = "v2.2.0"
	ctx := identityTestContext(t, ledger)
	identity, err := rememberLedgerV3Identity(ctx, stack, ledger)
	require.NoError(t, err)
	require.Empty(t, identity)
	saved := &v1beta1.Ledger{}
	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), saved))
	require.Empty(t, saved.Annotations)
}

type identityReaderContext struct {
	reindexTestContext
	apiReader client.Reader
}

func (c identityReaderContext) GetAPIReader() client.Reader { return c.apiReader }

func TestLedgerV3IdentityDisableReadsPastCacheMiss(t *testing.T) {
	old := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = old })
	stack, ledger, cluster := identityTestObjects(t)
	stack.Spec.Disabled = true
	ctx := identityTestContext(t, stack, ledger, cluster)
	directReader := ctx.client
	ctx.client = interceptor.NewClient(ctx.client.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
		if o.GetObjectKind().GroupVersionKind() == ledgerV3ClusterGVK {
			return apierrors.NewNotFound(schema.GroupResource{Group: "ledger.formance.com", Resource: "clusters"}, key.Name)
		}
		return c.Get(ctx, key, o, opts...)
	}})
	require.NoError(t, reconcileLedgerModule(identityReaderContext{reindexTestContext: ctx, apiReader: directReader}, stack, identityOptions(), ledger))
	saved := &v1beta1.Ledger{}
	require.NoError(t, directReader.Get(ctx, client.ObjectKeyFromObject(ledger), saved))
	require.Equal(t, identityTestUUID, saved.Annotations[ledgerV3IdentityAnnotation])
	require.True(t, apierrors.IsNotFound(directReader.Get(ctx, client.ObjectKeyFromObject(cluster), newV3Cluster())))
}

func TestLedgerV3IdentityLegacyDisableKeepsExistingBehavior(t *testing.T) {
	old := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = old })
	stack, ledger, _ := identityTestObjects(t)
	stack.Spec.Version = "v2.2.0"
	stack.Spec.Disabled = true
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "ledger", Namespace: stack.Name}}
	database := &v1beta1.Database{ObjectMeta: metav1.ObjectMeta{Name: "ledger-database"}}
	ctx := identityTestContext(t, stack, ledger)
	require.NoError(t, controllerutil.SetControllerReference(ledger, deployment, ctx.GetScheme()))
	require.NoError(t, controllerutil.SetControllerReference(ledger, database, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, deployment))
	require.NoError(t, ctx.GetClient().Create(ctx, database))
	options := identityOptions()
	options.Owns[&appsv1.Deployment{}] = nil
	options.Owns[&v1beta1.Database{}] = nil
	require.NoError(t, reconcileLedgerModule(ctx, stack, options, ledger))
	require.True(t, apierrors.IsNotFound(ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(deployment), &appsv1.Deployment{})))
	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(database), &v1beta1.Database{}))
	saved := &v1beta1.Ledger{}
	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), saved))
	require.Empty(t, saved.Annotations)
}

func TestLedgerV3IdentityMissingChildConfiguredConflict(t *testing.T) {
	stack, ledger, _ := identityTestObjects(t)
	ledger.Annotations = map[string]string{ledgerV3IdentityAnnotation: identityTestUUID}
	config := ledgerConfiguration("default", 0, "*")
	config.Spec.Cluster.ClusterID = "different"
	ctx := identityTestContext(t, ledger, config)
	_, _, err := createOrUpdateV3Cluster(ctx, stack, ledger, "v3.0.0-rc.1", false, "")
	require.ErrorContains(t, err, "clusterID conflict")
	require.True(t, apierrors.IsNotFound(ctx.GetClient().Get(ctx, client.ObjectKey{Name: stack.Name, Namespace: stack.Name}, newV3Cluster())))
}

func TestLedgerV3IdentityDisableWaitsForGeneratedID(t *testing.T) {
	old := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = old })
	for _, moduleDisabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("module-disabled-%t", moduleDisabled), func(t *testing.T) {
			stack, ledger, cluster := identityTestObjects(t)
			stack.Spec.Disabled = !moduleDisabled
			ledger.Spec.Disabled = moduleDisabled
			unstructured.RemoveNestedField(cluster.Object, "spec", "clusterID")
			ctx := identityTestContext(t, stack, ledger, cluster)
			require.ErrorContains(t, reconcileLedgerModule(ctx, stack, identityOptions(), ledger), "waiting for owned Ledger v3 Cluster clusterID")
			retained := newV3Cluster()
			require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), retained))
			require.NoError(t, unstructured.SetNestedField(retained.Object, identityTestUUID, "spec", "clusterID"))
			require.NoError(t, ctx.GetClient().Update(ctx, retained))
			require.NoError(t, reconcileLedgerModule(ctx, stack, identityOptions(), ledger))
			require.True(t, apierrors.IsNotFound(ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), newV3Cluster())))
			saved := &v1beta1.Ledger{}
			require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(ledger), saved))
			require.Equal(t, identityTestUUID, saved.Annotations[ledgerV3IdentityAnnotation])
			require.Equal(t, saved.ResourceVersion, ledger.ResourceVersion)
		})
	}
}

func TestLedgerV3IdentityInitialCreationCanAwaitGeneratedID(t *testing.T) {
	stack, ledger, cluster := identityTestObjects(t)
	unstructured.RemoveNestedField(cluster.Object, "spec", "clusterID")
	ctx := identityTestContext(t, ledger, cluster)
	_, spec, err := createOrUpdateV3Cluster(ctx, stack, ledger, "v3.0.0-rc.1", false, "")
	require.NoError(t, err)
	require.Empty(t, spec.ClusterID)
}
