package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
