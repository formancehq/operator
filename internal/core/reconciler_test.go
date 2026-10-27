package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

type reconcileTestManager struct {
	ctrl.Manager
	client client.Client
	scheme *runtime.Scheme
}

func (m reconcileTestManager) GetClient() client.Client    { return m.client }
func (m reconcileTestManager) GetAPIReader() client.Reader { return m.client }
func (m reconcileTestManager) GetScheme() *runtime.Scheme  { return m.scheme }
func (m reconcileTestManager) GetPlatform() Platform       { return Platform{} }

func TestReconcileObjectReturnsApplicationErrorRequeueDelay(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))

	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(connectivity).
		WithObjects(connectivity).
		Build()
	mgr := reconcileTestManager{client: fakeClient, scheme: scheme}

	delay := 17 * time.Second
	controller := ForObjectController(func(Context, *ReconcilerOptions[*v1beta1.Connectivity], *v1beta1.Connectivity) error {
		return NewPendingError().
			WithMessage("waiting for dependency").
			WithRequeueAfter(delay)
	})

	result, err := reconcileObject[*v1beta1.Connectivity](mgr, controller, ReconcilerOptions[*v1beta1.Connectivity]{})(
		context.Background(),
		reconcile.Request{NamespacedName: client.ObjectKey{Name: connectivity.Name}},
	)

	require.NoError(t, err)
	require.Equal(t, delay, result.RequeueAfter,
		"a pending application error with a retry delay must schedule another reconcile")

	updated := &v1beta1.Connectivity{}
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{Name: connectivity.Name}, updated))
	require.False(t, updated.Status.Ready)
	require.Equal(t, "waiting for dependency", updated.Status.Info)
}

func TestReconcileObjectPreservesForeignFinalizers(t *testing.T) {
	for _, scenario := range []string{"normal", "pending", "error", "complete"} {
		t.Run(scenario, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, v1beta1.AddToScheme(scheme))
			module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", Finalizers: []string{"foreign.example/cleanup", "owned"}}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(module).WithObjects(module).Build()
			if scenario != "normal" {
				require.NoError(t, c.Delete(context.Background(), module))
			}
			options := ReconcilerOptions[*v1beta1.Connectivity]{}
			WithFinalizer("owned", func(Context, *v1beta1.Connectivity) error {
				switch scenario {
				case "pending":
					return NewPendingError().WithMessage("waiting for acknowledgement")
				case "error":
					return errors.New("unavailable")
				}
				return nil
			})(&options)
			controller := ForObjectController(func(Context, *ReconcilerOptions[*v1beta1.Connectivity], *v1beta1.Connectivity) error {
				require.Equal(t, "normal", scenario, "normal controller must not run during deletion")
				return nil
			})
			result, err := reconcileObject(reconcileTestManager{client: c, scheme: scheme}, controller, options)(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(module)})
			if scenario == "error" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if scenario == "pending" {
				require.Positive(t, result.RequeueAfter)
			}
			got := &v1beta1.Connectivity{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(module), got))
			require.Contains(t, got.Finalizers, "foreign.example/cleanup")
			if scenario == "complete" {
				require.NotContains(t, got.Finalizers, "owned")
			} else {
				require.Contains(t, got.Finalizers, "owned")
			}
		})
	}
}

func TestReconcileObjectFinalizerInstallationRejectsConcurrentEdit(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	module := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", Finalizers: []string{"foreign.example/first"}}}
	underlying := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(module).WithObjects(module).Build()
	c := interceptor.NewClient(underlying, interceptor.Funcs{Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		concurrent := &v1beta1.Connectivity{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(module), concurrent))
		concurrent.Finalizers = append(concurrent.Finalizers, "foreign.example/concurrent")
		require.NoError(t, cl.Update(ctx, concurrent))
		return cl.Patch(ctx, obj, patch, opts...)
	}})
	options := ReconcilerOptions[*v1beta1.Connectivity]{}
	WithFinalizer("owned", func(Context, *v1beta1.Connectivity) error { return nil })(&options)
	controller := func(Context, *ReconcilerOptions[*v1beta1.Connectivity], *v1beta1.Connectivity) error {
		t.Fatal("controller ran after conflict")
		return nil
	}
	_, err := reconcileObject(reconcileTestManager{client: c, scheme: scheme}, controller, options)(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(module)})
	require.True(t, apierrors.IsConflict(err), "%v", err)
	got := &v1beta1.Connectivity{}
	require.NoError(t, underlying.Get(context.Background(), client.ObjectKeyFromObject(module), got))
	require.Equal(t, []string{"foreign.example/first", "foreign.example/concurrent"}, got.Finalizers)
}
