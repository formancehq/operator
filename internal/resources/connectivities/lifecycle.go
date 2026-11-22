package connectivities

import (
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
)

// Shared with the delegated controller. Its presence alone does not establish
// runtime compatibility: the matching CRD and controller must be rolled out first.
const connectivityLifecycleFinalizer = "connectivity.formance.com/connectivity"

func lifecyclePending(module *v1beta1.Connectivity, reason, message string) error {
	setCondition(module, metav1.ConditionFalse, reason, message)
	return NewPendingError().WithMessage("%s", message).WithRequeueAfter(connectivityAPIRetryDelay)
}

// Connectivity delegates disable instead of using the shared module deletion path.
func reconcileConnectivityModule(ctx Context, stack *v1beta1.Stack, module *v1beta1.Connectivity) error {
	if stack.Spec.Disabled || module.Spec.Disabled {
		owned, err := HasOwnerReference(ctx, stack, module)
		if err != nil {
			return err
		}
		if !owned {
			patch := client.MergeFrom(module.DeepCopy())
			if err := controllerutil.SetOwnerReference(stack, module, ctx.GetScheme()); err != nil {
				return err
			}
			if err := ctx.GetClient().Patch(ctx, module, patch); err != nil {
				return err
			}
		}
		module.GetConditions().Delete(v1beta1.ConditionTypeMatch(DependenciesSatisfiedCondition))
		return disableConnectivity(ctx, stack, nil, module)
	}
	return ForModule(connectivityModuleRequirements(), func(ctx Context, stack *v1beta1.Stack, _ *ReconcilerOptions[*v1beta1.Connectivity], module *v1beta1.Connectivity, version string) error {
		return Reconcile(ctx, stack, module, version)
	})(ctx, stack, &ReconcilerOptions[*v1beta1.Connectivity]{
		UnsatisfiedRequirementsHandle: handleUnsatisfiedLedgerRequirement,
	}, module)
}

func disableConnectivity(ctx Context, stack *v1beta1.Stack, _ *ReconcilerOptions[*v1beta1.Connectivity], module *v1beta1.Connectivity) error {
	if err := deleteOwnedGatewayHTTPAPI(ctx, module); err != nil {
		return err
	}
	delegated := &unstructured.Unstructured{}
	delegated.SetGroupVersionKind(connectivityGVK)
	if err := ctx.GetAPIReader().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, delegated); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return lifecyclePending(module, "DisableLookupFailed", fmt.Sprintf("cannot read delegated Connectivity: %v", err))
	}
	if !controllerRefMatches(metav1.GetControllerOf(delegated), v1beta1.GroupVersion.WithKind("Connectivity"), module) {
		return lifecyclePending(module, "DelegatedOwnershipConflict", "delegated Connectivity is not controlled by this module")
	}
	if !delegated.GetDeletionTimestamp().IsZero() {
		return lifecyclePending(module, "DelegatedDeletionPending", "delegated Connectivity is already deleting; disable cannot restore it")
	}
	disabled, _, err := unstructured.NestedBool(delegated.Object, "spec", "disabled")
	if err != nil {
		return err
	}
	if !disabled {
		// Test both identity and version before changing a single field. This rejects
		// concurrent edits and same-name replacements read through the direct reader.
		guarded, err := json.Marshal([]map[string]any{
			{"op": "test", "path": "/metadata/uid", "value": string(delegated.GetUID())},
			{"op": "test", "path": "/metadata/resourceVersion", "value": delegated.GetResourceVersion()},
			{"op": "add", "path": "/spec/disabled", "value": true},
		})
		if err != nil {
			return err
		}
		if err := ctx.GetClient().Patch(ctx, delegated, client.RawPatch(types.JSONPatchType, guarded)); err != nil {
			return err
		}
	}
	setCondition(module, metav1.ConditionTrue, "DisableRequested", "Connectivity disable requested; the Connectivity controller owns workload shutdown")
	module.GetConditions().AppendOrReplace(v1beta1.Condition{Type: "ReconciledWithStack", Status: metav1.ConditionTrue, ObservedGeneration: stack.Generation, LastTransitionTime: metav1.Now(), Reason: "Spec", Message: "Connectivity disable requested with Stack"}, v1beta1.ConditionTypeMatch("ReconciledWithStack"))
	return nil
}

func finalizeConnectivity(ctx Context, module *v1beta1.Connectivity) error {
	// Keep the existing credential finalizer, without waiting for delegated cleanup.
	return teardownDelegated(ctx, &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: module.GetStack()}}, module)
}
