package connectivities

import (
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func requireDisabledSchema(ctx Context, module *v1beta1.Connectivity) error {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := ctx.GetAPIReader().Get(ctx, client.ObjectKey{Name: "connectivities.connectivity.formance.com"}, crd); err != nil {
		return lifecyclePending(module, "LifecycleCapabilityUnavailable", fmt.Sprintf("cannot verify Connectivity lifecycle CRD: %v; install compatible CRDs and controller first", err))
	}
	for _, version := range crd.Spec.Versions {
		if version.Name != connectivityGVK.Version || !version.Served || version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			continue
		}
		spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
		if spec.Properties["disabled"].Type == "boolean" {
			return nil
		}
	}
	return lifecyclePending(module, "LifecycleCapabilityUnavailable", "Connectivity CRD lacks boolean spec.disabled; install compatible CRDs and controller first")
}

func disableConnectivity(ctx Context, stack *v1beta1.Stack, module *v1beta1.Connectivity) error {
	if err := deleteOwnedGatewayHTTPAPI(ctx, module); err != nil {
		return err
	}
	if err := requireDisabledSchema(ctx, module); err != nil {
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
		return lifecyclePending(module, "Disabling", "waiting for delegated Connectivity to stop Core, API and all ConnectorInstance workloads")
	}
	phase, _, err := unstructured.NestedString(delegated.Object, "status", "phase")
	if err != nil {
		return err
	}
	address, _, err := unstructured.NestedString(delegated.Object, "status", "address")
	if err != nil {
		return err
	}
	observed, found, err := unstructured.NestedInt64(delegated.Object, "status", "observedGeneration")
	if err != nil {
		return err
	}
	if phase != "Disabled" || address != "" || !found || observed != delegated.GetGeneration() {
		return lifecyclePending(module, "Disabling", "waiting for delegated Connectivity Disabled phase and empty address")
	}
	setCondition(module, metav1.ConditionTrue, "Disabled", "Connectivity workloads are disabled; configuration and credentials are retained")
	module.GetConditions().AppendOrReplace(v1beta1.Condition{Type: "ReconciledWithStack", Status: metav1.ConditionTrue, ObservedGeneration: stack.Generation, LastTransitionTime: metav1.Now(), Reason: "Spec", Message: "Connectivity disabled with Stack"}, v1beta1.ConditionTypeMatch("ReconciledWithStack"))
	return nil
}

func finalizeConnectivity(ctx Context, module *v1beta1.Connectivity) error {
	// Keep the existing finalizer name: objects created by older Stack Operators
	// must receive this barrier before their credential can be revoked.
	return teardownDelegated(ctx, &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: module.GetStack()}}, module)
}
