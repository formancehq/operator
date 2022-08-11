package internal

import (
	"context"
	"reflect"

	. "github.com/numary/formance-operator/internal/collectionutil"
	pkgError "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Condition interface {
	GetType() string
	GetStatus() metav1.ConditionStatus
	GetObservedGeneration() int64
}

type Object[T Condition] interface {
	client.Object
	GetConditions() []T
}

type Mutator[COND Condition, T Object[COND]] interface {
	SetupWithBuilder(builder *ctrl.Builder)
	Mutate(ctx context.Context, t T) (*ctrl.Result, error)
}

// Reconciler reconciles a Stack object
type Reconciler[COND Condition, T Object[COND]] struct {
	client.Client
	Scheme  *runtime.Scheme
	Mutator Mutator[COND, T]
}

func (r *Reconciler[COND, T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log.FromContext(ctx).Info("Starting reconciliation")
	defer func() {
		log.FromContext(ctx).Info("Reconciliation terminated")
	}()

	var t T
	t = reflect.New(reflect.TypeOf(t).Elem()).Interface().(T)
	if err := r.Get(ctx, req.NamespacedName, t); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, pkgError.Wrap(err, "Reading target")
	}
	actual := t.DeepCopyObject().(T)
	updated := t.DeepCopyObject().(T)

	result, reconcileError := r.Mutator.Mutate(ctx, updated)
	if reconcileError != nil {
		log.FromContext(ctx).Error(reconcileError, "Reconciling")
	}

	conditionsChanged := len(actual.GetConditions()) != len(updated.GetConditions())
	if !conditionsChanged {
		for _, condition := range actual.GetConditions() {
			v := First(updated.GetConditions(), func(c COND) bool {
				return c.GetType() == condition.GetType()
			})
			if v == nil {
				conditionsChanged = true
				break
			}
			if (*v).GetStatus() != condition.GetStatus() {
				conditionsChanged = true
				break
			}
			if (*v).GetObservedGeneration() != condition.GetObservedGeneration() {
				conditionsChanged = true
				break
			}
		}
	}

	if conditionsChanged {
		log.FromContext(ctx).Info("Conditions changed, updating status")
		if patchErr := r.Status().Update(ctx, updated); patchErr != nil {
			log.FromContext(ctx).Error(patchErr, "Updating status")
			return ctrl.Result{}, patchErr
		}
	}

	if result != nil {
		return *result, nil
	}

	return ctrl.Result{
		Requeue: reconcileError != nil,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler[COND, T]) SetupWithManager(mgr ctrl.Manager) error {
	var t T
	t = reflect.New(reflect.TypeOf(t).Elem()).Interface().(T)
	builder := ctrl.NewControllerManagedBy(mgr).For(t)
	r.Mutator.SetupWithBuilder(builder)
	return builder.Complete(r)
}

func NewReconciler[COND Condition, T Object[COND]](client client.Client, scheme *runtime.Scheme, mutator Mutator[COND, T]) *Reconciler[COND, T] {
	return &Reconciler[COND, T]{
		Client:  client,
		Scheme:  scheme,
		Mutator: mutator,
	}
}
