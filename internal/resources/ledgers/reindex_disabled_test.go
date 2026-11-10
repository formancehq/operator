package ledgers

import (
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestReindexCronJobSearchDisableReenable(t *testing.T) {
	t.Parallel()
	ledger := reindexTestLedger()
	search := &v1beta1.Search{
		ObjectMeta: metav1.ObjectMeta{Name: "search", UID: "search-uid"},
		Spec:       v1beta1.SearchSpec{StackDependency: v1beta1.StackDependency{Stack: ledger.Spec.Stack}},
	}
	ctx := newReindexTestContext(t, search)
	require.NoError(t, fake.AddIndex(ctx.GetClient(), &v1beta1.Search{}, "stack", func(object client.Object) []string {
		return []string{object.(*v1beta1.Search).Spec.Stack}
	}))
	key := client.ObjectKey{Namespace: ledger.Spec.Stack, Name: "reindex-ledger"}

	// Enabled Search creates the existing suspended reindex CronJob.
	require.NoError(t, reconcileReindexCronJob(ctx, ledger.Spec.Stack, ledger))
	cronJob := storedReindexCronJob(t, ctx, ledger)
	require.NotNil(t, cronJob.Spec.Suspend)
	require.True(t, *cronJob.Spec.Suspend)
	require.True(t, metav1.IsControlledBy(cronJob, ledger))

	// Retaining the disabled Search must not retain its reindex CronJob.
	search.Spec.Disabled = true
	require.NoError(t, ctx.GetClient().Update(ctx, search))
	require.NoError(t, reconcileReindexCronJob(ctx, ledger.Spec.Stack, ledger))
	require.True(t, apierrors.IsNotFound(ctx.GetClient().Get(ctx, key, &batchv1.CronJob{})))
	retained := &v1beta1.Search{}
	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(search), retained))
	require.Equal(t, search.UID, retained.UID)
	require.Equal(t, search.Spec, retained.Spec)

	search.Spec.Disabled = false
	require.NoError(t, ctx.GetClient().Update(ctx, search))
	require.NoError(t, reconcileReindexCronJob(ctx, ledger.Spec.Stack, ledger))
	require.True(t, *storedReindexCronJob(t, ctx, ledger).Spec.Suspend)
}

func TestReindexCronJobAbsentSearchRemovesExisting(t *testing.T) {
	t.Parallel()
	ledger := reindexTestLedger()
	ctx := newReindexTestContext(t)
	require.NoError(t, fake.AddIndex(ctx.GetClient(), &v1beta1.Search{}, "stack", func(object client.Object) []string {
		return []string{object.(*v1beta1.Search).Spec.Stack}
	}))
	_, err := createReindexCronJob(ctx, ledger)
	require.NoError(t, err)
	require.NoError(t, reconcileReindexCronJob(ctx, ledger.Spec.Stack, ledger))
	require.True(t, apierrors.IsNotFound(ctx.GetClient().Get(ctx, client.ObjectKey{
		Namespace: ledger.Spec.Stack, Name: "reindex-ledger",
	}, &batchv1.CronJob{})))
}
