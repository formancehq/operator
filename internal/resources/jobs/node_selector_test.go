package jobs

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

type nodeSelectorContext struct {
	context.Context
	client client.Client
}

func (c nodeSelectorContext) GetClient() client.Client    { return c.client }
func (c nodeSelectorContext) GetScheme() *runtime.Scheme  { return c.client.Scheme() }
func (c nodeSelectorContext) GetAPIReader() client.Reader { return c.client }
func (c nodeSelectorContext) GetPlatform() core.Platform  { return core.Platform{} }

func newNodeSelectorContext(t *testing.T, objects ...client.Object) nodeSelectorContext {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kubernetesClient := fake.NewClientBuilder().WithScheme(scheme).
		WithIndex(&v1beta1.Settings{}, "stack", func(object client.Object) []string {
			return object.(*v1beta1.Settings).GetStacks()
		}).
		WithIndex(&v1beta1.Settings{}, "keylen", func(object client.Object) []string {
			return []string{strconv.Itoa(len(settings.SplitKeywordWithDot(object.(*v1beta1.Settings).Spec.Key)))}
		}).WithObjects(objects...).Build()
	return nodeSelectorContext{Context: t.Context(), client: kubernetesClient}
}

func TestHandleNodeSelector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		settings []client.Object
		existing map[string]string
		want     map[string]string
		wantErr  string
	}{
		{name: "unset preserves scheduling"},
		{name: "unset preserves existing selectors", existing: map[string]string{"disk": "ssd"}, want: map[string]string{"disk": "ssd"}},
		{
			name:     "wildcard applies on-demand and architecture selectors",
			settings: []client.Object{settings.New("all-jobs", "jobs.nodeSelector", "karpenter.sh/capacity-type=on-demand,kubernetes.io/arch=arm64", "*")},
			want:     map[string]string{"karpenter.sh/capacity-type": "on-demand", "kubernetes.io/arch": "arm64"},
		},
		{
			name: "stack setting overrides wildcard map",
			settings: []client.Object{
				settings.New("all-jobs", "jobs.nodeSelector", "karpenter.sh/capacity-type=spot", "*"),
				settings.New("stack-jobs", "jobs.nodeSelector", "karpenter.sh/capacity-type=on-demand", "stack0"),
			},
			want: map[string]string{"karpenter.sh/capacity-type": "on-demand"},
		},
		{
			name:     "setting for another stack is ignored",
			settings: []client.Object{settings.New("other-jobs", "jobs.nodeSelector", "karpenter.sh/capacity-type=on-demand", "other")},
		},
		{
			name:     "merges existing selectors and accepts identical values",
			settings: []client.Object{settings.New("all-jobs", "jobs.nodeSelector", "karpenter.sh/capacity-type=on-demand", "*")},
			existing: map[string]string{"disk": "ssd", "karpenter.sh/capacity-type": "on-demand"},
			want:     map[string]string{"disk": "ssd", "karpenter.sh/capacity-type": "on-demand"},
		},
		{
			name:     "conflict prevents creation",
			settings: []client.Object{settings.New("all-jobs", "jobs.nodeSelector", "karpenter.sh/capacity-type=on-demand", "*")},
			existing: map[string]string{"karpenter.sh/capacity-type": "spot"},
			wantErr:  "conflicts with pod node selector",
		},
		{
			name:     "malformed map prevents creation",
			settings: []client.Object{settings.New("all-jobs", "jobs.nodeSelector", "broken", "*")},
			wantErr:  "reading jobs.nodeSelector",
		},
		{
			name:     "invalid label key prevents creation",
			settings: []client.Object{settings.New("all-jobs", "jobs.nodeSelector", "bad key=on-demand", "*")},
			wantErr:  "invalid jobs.nodeSelector",
		},
		{
			name:     "invalid label value prevents creation",
			settings: []client.Object{settings.New("all-jobs", "jobs.nodeSelector", "disk=bad value", "*")},
			wantErr:  "invalid jobs.nodeSelector",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := newNodeSelectorContext(t, tt.settings...)
			owner := &v1beta1.Ledger{
				TypeMeta:   metav1.TypeMeta{APIVersion: v1beta1.GroupVersion.String(), Kind: "Ledger"},
				ObjectMeta: metav1.ObjectMeta{Name: "ledger", UID: types.UID("owner")},
				Spec:       v1beta1.LedgerSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
			}
			err := Handle(ctx, owner, "test", corev1.Container{Name: "test", Image: "test"}, Mutator(func(job *batchv1.Job) error {
				job.Spec.Template.Spec.NodeSelector = maps.Clone(tt.existing)
				return nil
			}))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Handle() error = %v, want %q", err, tt.wantErr)
				}
			} else if err == nil || err.Error() != core.NewPendingError().Error() {
				t.Fatalf("Handle() error = %v, want pending", err)
			}
			list := &batchv1.JobList{}
			if err := ctx.client.List(ctx, list); err != nil {
				t.Fatal(err)
			}
			if tt.wantErr != "" {
				if len(list.Items) != 0 {
					t.Fatal("invalid setting must not create a Job")
				}
				return
			}
			if len(list.Items) != 1 {
				t.Fatalf("created %d Jobs, want 1", len(list.Items))
			}
			if got := list.Items[0].Spec.Template.Spec.NodeSelector; !maps.Equal(got, tt.want) {
				t.Fatalf("nodeSelector = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleNodeSelectorPreservesExistingJobs(t *testing.T) {
	t.Parallel()
	for _, succeeded := range []int32{0, 1} {
		t.Run(strconv.Itoa(int(succeeded)), func(t *testing.T) {
			t.Parallel()
			container := corev1.Container{Name: "test", Image: "test"}
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "owner-test", Namespace: "stack0", UID: "existing"},
				Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers:   []corev1.Container{container},
					NodeSelector: map[string]string{"karpenter.sh/capacity-type": "spot"},
				}}},
				Status: batchv1.JobStatus{Succeeded: succeeded},
			}
			ctx := newNodeSelectorContext(t, job,
				settings.New("all-jobs", "jobs.nodeSelector", "karpenter.sh/capacity-type=on-demand", "*"))
			owner := &v1beta1.Ledger{
				ObjectMeta: metav1.ObjectMeta{Name: "ledger", UID: "owner"},
				Spec:       v1beta1.LedgerSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
			}
			err := Handle(ctx, owner, "test", container)
			if succeeded > 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != core.NewPendingError().Error() {
				t.Fatalf("Handle() error = %v, want pending", err)
			}
			stored := &batchv1.Job{}
			if err := ctx.client.Get(ctx, client.ObjectKeyFromObject(job), stored); err != nil {
				t.Fatal(err)
			}
			if stored.UID != job.UID || stored.ResourceVersion != job.ResourceVersion {
				t.Fatal("setting changes must not replace or update an existing Job")
			}
			if !maps.Equal(stored.Spec.Template.Spec.NodeSelector, job.Spec.Template.Spec.NodeSelector) {
				t.Fatal("setting changes must not modify an existing Job's selectors")
			}
		})
	}
}
