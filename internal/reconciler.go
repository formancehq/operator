package internal

import (
	"context"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	"github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
	pkgError "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ConditionTypeError = "Error"
)

type Mutator[T sharedtypes.Object] interface {
	SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error
	Mutate(ctx context.Context, t T) (*ctrl.Result, error)
}

// Reconciler reconciles a Stack object
type Reconciler[T sharedtypes.Object] struct {
	client.Client
	Scheme  *runtime.Scheme
	Mutator Mutator[T]
}

func (r *Reconciler[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log.FromContext(ctx).Info("Starting reconciliation")
	defer func() {
		log.FromContext(ctx).Info("Reconciliation terminated")
	}()

	var t T
	t = reflect.New(reflect.TypeOf(t).Elem()).Interface().(T)
	if err := r.Get(ctx, req.NamespacedName, t); err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Object not found, skip")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, pkgError.Wrap(err, "Reading target")
	}
	actual := t.DeepCopyObject().(T)
	updated := t.DeepCopyObject().(T)

	log.FromContext(ctx).Info("Call mutator")
	result, reconcileError := r.Mutator.Mutate(ctx, updated)
	if reconcileError != nil {
		sharedtypes.SetCondition(updated, ConditionTypeError, metav1.ConditionTrue, reconcileError.Error())
		log.FromContext(ctx).Error(reconcileError, "Reconciling")
	} else {
		updated.GetConditions().Remove(ConditionTypeError)
	}

	conditionsChanged := len(*actual.GetConditions()) != len(*updated.GetConditions())
	if !conditionsChanged {
		for _, condition := range *actual.GetConditions() {
			v := First(*updated.GetConditions(), func(c sharedtypes.Condition) bool {
				return c.Type == condition.Type
			})
			if v == nil {
				conditionsChanged = true
				break
			}
			if (*v).Status != condition.Status {
				conditionsChanged = true
				break
			}
			if (*v).ObservedGeneration != condition.ObservedGeneration {
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

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler[T]) SetupWithManager(mgr ctrl.Manager) error {
	var t T
	t = reflect.New(reflect.TypeOf(t).Elem()).Interface().(T)

	gvk, err := apiutil.GVKForObject(t, r.Scheme)
	if err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(t).
		WithLogConstructor(func(req *reconcile.Request) logr.Logger {
			log := mgr.GetLogger().WithValues(
				"controller", strings.ToLower(gvk.Kind),
				"controllerGroup", gvk.Group,
				"controllerKind", gvk.Kind,
			)

			lowerCamelCaseKind := strings.ToLower(gvk.Kind[:1]) + gvk.Kind[1:]

			if req != nil {
				log = log.WithValues(
					lowerCamelCaseKind, klog.KRef(req.Namespace, req.Name),
					"namespace", req.Namespace, "name", req.Name,
				)
			}

			return log
		})
	if err := r.Mutator.SetupWithBuilder(mgr, builder); err != nil {
		return err
	}

	return builder.Complete(r)
}

func NewReconciler[T sharedtypes.Object](client client.Client, scheme *runtime.Scheme, mutator Mutator[T]) *Reconciler[T] {
	return &Reconciler[T]{
		Client:  client,
		Scheme:  scheme,
		Mutator: mutator,
	}
}
