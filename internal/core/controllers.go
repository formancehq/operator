package core

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

// ObjectReconcilerFunc adapts a function into a reconcile.ObjectReconciler[T].
type ObjectReconcilerFunc[T client.Object] func(context.Context, T) (reconcile.Result, error)

func (f ObjectReconcilerFunc[T]) Reconcile(ctx context.Context, obj T) (reconcile.Result, error) {
	return f(ctx, obj)
}

func ForObjectController[T v1beta1.Object](controller reconcile.ObjectReconciler[T]) reconcile.ObjectReconciler[T] {
	return ObjectReconcilerFunc[T](func(ctx context.Context, object T) (reconcile.Result, error) {
		setStatus := func(err error) {
			if err != nil {
				object.SetReady(false)
				object.SetError(err.Error())
			} else {
				object.SetReady(true)
				object.SetError("Up to date")
			}
		}

		result, err := controller.Reconcile(ctx, object)
		if err != nil {
			setStatus(err)
			if !IsApplicationError(err) {
				return result, err
			}
			return result, nil
		}

		for _, condition := range *object.GetConditions() {
			if condition.ObservedGeneration != object.GetGeneration() {
				continue
			}

			if condition.Status != metav1.ConditionTrue {
				str := condition.Type
				if condition.Reason != "" {
					str += "/" + condition.Reason
				}

				setStatus(NewPendingError().WithMessage("%s", "pending condition: "+str))
				return reconcile.Result{}, nil
			}
		}
		setStatus(nil)

		return result, nil
	})
}

type StackDependentObjectController[T v1beta1.Dependent] func(ctx context.Context, stack *v1beta1.Stack, req T) error

func ForStackDependency[T v1beta1.Dependent](ctrl StackDependentObjectController[T], allowDeleted bool) reconcile.ObjectReconciler[T] {
	return ObjectReconcilerFunc[T](func(ctx context.Context, t T) (reconcile.Result, error) {
		stack := &v1beta1.Stack{}
		if err := GetClient(ctx).Get(ctx, types.NamespacedName{
			Name: t.GetStack(),
		}, stack); err != nil {
			if apierrors.IsNotFound(err) {
				return reconcile.Result{}, NewStackNotFoundError()
			}
			return reconcile.Result{}, err
		}

		if stack.GetAnnotations()[v1beta1.SkipLabel] == "true" {
			t.GetConditions().
				AppendOrReplace(v1beta1.Condition{
					Type:               "ReconciledWithStack",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: stack.GetGeneration(),
					LastTransitionTime: metav1.Now(),
					Message:            "Reconciled with stack specification",
					Reason:             "Skipped",
				}, v1beta1.ConditionTypeMatch("ReconciledWithStack"))
			return reconcile.Result{}, nil
		}

		if !allowDeleted {
			if !stack.GetDeletionTimestamp().IsZero() {
				return reconcile.Result{}, NewStackNotFoundError()
			}
		}

		return reconcile.Result{}, ctrl(ctx, stack, t)
	})
}

type ModuleController[T v1beta1.Module] func(ctx context.Context, stack *v1beta1.Stack, req T, version string) error

func ForModule[T v1beta1.Module](underlyingController ModuleController[T]) StackDependentObjectController[T] {
	return func(ctx context.Context, stack *v1beta1.Stack, t T) error {

		moduleVersion, err := GetModuleVersion(ctx, stack, t)
		if err != nil {
			return err
		}

		hasOwnerReference, err := HasOwnerReference(ctx, stack, t)
		if err != nil {
			return err
		}

		if !hasOwnerReference {
			patch := client.MergeFrom(t.DeepCopyObject().(T))
			if err := controllerutil.SetOwnerReference(stack, t, GetScheme(ctx)); err != nil {
				return err
			}
			if err := GetClient(ctx).Patch(ctx, t, patch); err != nil {
				return errors.Wrap(err, "patching object to add owner reference on stack")
			}
			log.FromContext(ctx).Info("Add owner reference on stack")
		}

		if stack.Spec.Disabled {
			// notes(gfyrag): When disabling a stack, we remove all owned objects for modules.
			// Owned objects must be controlled by the module.
			// if not, they will not be automatically removed on stack removal.
			// resources objects (like Database and BrokerTopic) are not removed since we could re-enable the stack later.
			if err := removeAllModulesOwnedObjects(ctx, t, ownedObjectsFromContext(ctx)); err != nil {
				return err
			}
		} else {
			err = underlyingController(ctx, stack, t, moduleVersion)
			if err != nil {
				return err
			}
		}

		t.GetConditions().AppendOrReplace(v1beta1.Condition{
			Type:               "ReconciledWithStack",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: stack.GetGeneration(),
			LastTransitionTime: metav1.Now(),
			Message:            "Reconciled with stack specification",
			Reason:             "Spec",
		}, v1beta1.ConditionTypeMatch("ReconciledWithStack"))

		return nil
	}
}

func removeAllModulesOwnedObjects(ctx Context, owner client.Object, owns map[client.Object][]builder.OwnsOption) error {
	logger := log.FromContext(ctx)
	stackName := ""
	if dep, ok := owner.(v1beta1.Dependent); ok {
		stackName = dep.GetStack()
	}

	ownerGVK, err := apiutil.GVKForObject(owner, GetScheme(ctx))
	if err != nil {
		return err
	}

	for object := range owns {
		if _, ok := object.(v1beta1.Resource); ok {
			// Resources must not be deleted
			continue
		}

		gvk, err := apiutil.GVKForObject(object, GetScheme(ctx))
		if err != nil {
			return err
		}

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)

		listOpts := []client.ListOption{}
		// Scope to the stack namespace for namespace-scoped resources (Deployments,
		// Services, etc.) to avoid listing objects cluster-wide.
		// Cluster-scoped Formance CRDs (group "formance.com") are not filtered by
		// namespace since they don't live in one.
		if stackName != "" && gvk.Group != "formance.com" {
			listOpts = append(listOpts, client.InNamespace(stackName))
		}
		if err := GetClient(ctx).List(ctx, list, listOpts...); err != nil {
			return err
		}

		for _, item := range list.Items {
			hasControllerReference, err := HasControllerReference(ctx, owner, &item)
			if err != nil {
				return err
			}
			if hasControllerReference {
				logger.Info(fmt.Sprintf("Deleting owned object %s %s/%s (owner: %s/%s)",
					gvk.Kind, item.GetNamespace(), item.GetName(),
					ownerGVK.Kind, owner.GetName()))
				if err := GetClient(ctx).Delete(ctx, &item); client.IgnoreNotFound(err) != nil {
					return err
				}
			}
		}
	}
	return nil
}
