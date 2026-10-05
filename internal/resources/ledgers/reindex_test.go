package ledgers

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

type reindexTestContext struct {
	context.Context
	client client.Client
}

var _ core.Context = reindexTestContext{}

func (c reindexTestContext) GetClient() client.Client    { return c.client }
func (c reindexTestContext) GetScheme() *runtime.Scheme  { return c.client.Scheme() }
func (c reindexTestContext) GetAPIReader() client.Reader { return c.client }
func (c reindexTestContext) GetPlatform() core.Platform  { return core.Platform{} }

func newReindexTestContext(t *testing.T, objects ...client.Object) reindexTestContext {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))
	kubernetesClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&v1beta1.Settings{}, "stack", func(object client.Object) []string {
			return object.(*v1beta1.Settings).GetStacks()
		}).
		WithIndex(&v1beta1.Settings{}, "keylen", func(object client.Object) []string {
			return []string{strconv.Itoa(len(strings.Split(object.(*v1beta1.Settings).Spec.Key, ".")))}
		}).
		WithObjects(objects...).
		Build()
	return reindexTestContext{Context: t.Context(), client: kubernetesClient}
}

func reindexTestLedger() *v1beta1.Ledger {
	return &v1beta1.Ledger{
		ObjectMeta: metav1.ObjectMeta{Name: "ledger", UID: "ledger-uid"},
		Spec:       v1beta1.LedgerSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
	}
}

func storedReindexCronJob(t *testing.T, ctx core.Context, ledger *v1beta1.Ledger) *batchv1.CronJob {
	t.Helper()
	cronJob := &batchv1.CronJob{}
	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKey{
		Namespace: ledger.Spec.Stack,
		Name:      "reindex-ledger",
	}, cronJob))
	return cronJob
}

func TestCreateReindexCronJobNodeSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		objects []client.Object
		want    map[string]string
	}{
		{name: "no setting"},
		{
			name: "on-demand with multiple selectors",
			objects: []client.Object{settings.New("wildcard", "jobs.nodeSelector",
				"karpenter.sh/capacity-type=on-demand,kubernetes.io/arch=arm64", "*")},
			want: map[string]string{"karpenter.sh/capacity-type": "on-demand", "kubernetes.io/arch": "arm64"},
		},
		{
			name: "stack setting overrides wildcard",
			objects: []client.Object{
				settings.New("wildcard", "jobs.nodeSelector", "karpenter.sh/capacity-type=spot,kubernetes.io/arch=amd64", "*"),
				settings.New("scoped", "jobs.nodeSelector", "karpenter.sh/capacity-type=on-demand,kubernetes.io/arch=arm64", "stack0"),
			},
			want: map[string]string{"karpenter.sh/capacity-type": "on-demand", "kubernetes.io/arch": "arm64"},
		},
		{
			name: "another stack setting is ignored",
			objects: []client.Object{settings.New("other", "jobs.nodeSelector",
				"karpenter.sh/capacity-type=on-demand", "stack1")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := newReindexTestContext(t, tt.objects...)
			ledger := reindexTestLedger()

			cronJob, err := createReindexCronJob(ctx, ledger)
			require.NoError(t, err)
			require.Equal(t, tt.want, cronJob.Spec.JobTemplate.Spec.Template.Spec.NodeSelector)
			stored := storedReindexCronJob(t, ctx, ledger)
			require.Equal(t, tt.want, stored.Spec.JobTemplate.Spec.Template.Spec.NodeSelector)
			require.True(t, metav1.IsControlledBy(stored, ledger))
		})
	}
}

func TestCreateReindexCronJobNodeSelectorUpdatesAndRemoval(t *testing.T) {
	t.Parallel()
	setting := settings.New("scoped", "jobs.nodeSelector",
		"karpenter.sh/capacity-type=on-demand,kubernetes.io/arch=arm64", "stack0")
	ctx := newReindexTestContext(t, setting)
	ledger := reindexTestLedger()

	_, err := createReindexCronJob(ctx, ledger)
	require.NoError(t, err)
	initial := storedReindexCronJob(t, ctx, ledger)
	require.Equal(t, map[string]string{
		"karpenter.sh/capacity-type": "on-demand", "kubernetes.io/arch": "arm64",
	}, initial.Spec.JobTemplate.Spec.Template.Spec.NodeSelector)

	require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(setting), setting))
	setting.Spec.Value = "karpenter.sh/capacity-type=spot"
	require.NoError(t, ctx.GetClient().Update(ctx, setting))
	_, err = createReindexCronJob(ctx, ledger)
	require.NoError(t, err)
	updated := storedReindexCronJob(t, ctx, ledger)
	require.Equal(t, map[string]string{"karpenter.sh/capacity-type": "spot"},
		updated.Spec.JobTemplate.Spec.Template.Spec.NodeSelector)
	require.NotEqual(t, initial.ResourceVersion, updated.ResourceVersion)
	require.Equal(t, initial.OwnerReferences, updated.OwnerReferences)

	require.NoError(t, ctx.GetClient().Delete(ctx, setting))
	_, err = createReindexCronJob(ctx, ledger)
	require.NoError(t, err)
	removed := storedReindexCronJob(t, ctx, ledger)
	require.Empty(t, removed.Spec.JobTemplate.Spec.Template.Spec.NodeSelector)
	require.NotEqual(t, updated.ResourceVersion, removed.ResourceVersion)
	require.Equal(t, initial.OwnerReferences, removed.OwnerReferences)
}

func TestCreateReindexCronJobMalformedNodeSelector(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"karpenter.sh/capacity-type",
		`karpenter.sh/capacity-type="on-demand`,
		"invalid key=on-demand",
		"karpenter.sh/capacity-type=invalid value",
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			t.Run("aborts creation", func(t *testing.T) {
				t.Parallel()
				ctx := newReindexTestContext(t, settings.New("scoped", "jobs.nodeSelector", value, "stack0"))
				ledger := reindexTestLedger()

				_, err := createReindexCronJob(ctx, ledger)
				require.ErrorContains(t, err, "jobs.nodeSelector")
				err = ctx.GetClient().Get(ctx, client.ObjectKey{
					Namespace: ledger.Spec.Stack, Name: "reindex-ledger",
				}, &batchv1.CronJob{})
				require.True(t, apierrors.IsNotFound(err), "CronJob must not be created: %v", err)
			})
			t.Run("preserves existing CronJob", func(t *testing.T) {
				t.Parallel()
				setting := settings.New("scoped", "jobs.nodeSelector", "karpenter.sh/capacity-type=on-demand", "stack0")
				ctx := newReindexTestContext(t, setting)
				ledger := reindexTestLedger()
				_, err := createReindexCronJob(ctx, ledger)
				require.NoError(t, err)
				stored := storedReindexCronJob(t, ctx, ledger)
				stored.Spec.Schedule = "0 2 * * *"
				require.NoError(t, ctx.GetClient().Update(ctx, stored))
				before := storedReindexCronJob(t, ctx, ledger)

				require.NoError(t, ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(setting), setting))
				setting.Spec.Value = value
				require.NoError(t, ctx.GetClient().Update(ctx, setting))
				_, err = createReindexCronJob(ctx, ledger)
				require.ErrorContains(t, err, "jobs.nodeSelector")
				require.Equal(t, before, storedReindexCronJob(t, ctx, ledger))
			})
		})
	}
}
