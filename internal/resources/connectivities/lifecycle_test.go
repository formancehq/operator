package connectivities

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func lifecycleCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "connectivities.connectivity.formance.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: connectivityGVK.Version, Served: true,
			Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {Properties: map[string]apiextensionsv1.JSONSchemaProps{"disabled": {Type: "boolean"}}},
			}}}}}},
	}
}

func lifecycleFixture(t *testing.T) (credsTestContext, *v1beta1.Stack, *v1beta1.Connectivity, *unstructured.Unstructured) {
	t.Helper()
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: "stack-uid", Generation: 4}, Spec: v1beta1.StackSpec{Disabled: true}}
	module := newConnectivity("stack0", stack.Name)
	module.UID = "module-uid"
	delegated := newDelegatedConnectivity(stack.Name)
	delegated.SetUID("delegated-uid")
	delegated.SetGeneration(8)
	delegated.SetFinalizers([]string{connectivityLifecycleFinalizer})
	delegated.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(module, v1beta1.GroupVersion.WithKind("Connectivity"))})
	require.NoError(t, unstructured.SetNestedMap(delegated.Object, map[string]any{"disabled": false, "ledgerAddress": "retained", "api": map[string]any{"enabled": true}}, "spec"))
	creds := newLedgerCredentialsForStack(stack.Name)
	setStackOwnedLedgerCredentials(stack, module, creds)
	route := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{Name: "stack0-connectivity", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(module, v1beta1.GroupVersion.WithKind("Connectivity"))}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "managed", Namespace: stack.Name, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(delegated, connectivityGVK)}}, Data: map[string][]byte{"key": []byte("retained")}}
	ctx := newReconcileTestContext(t, stack, module, delegated, creds, route, secret, lifecycleCRD())
	return ctx, stack, module, delegated
}

func TestDisableConnectivityRequestsShutdownWithoutWaiting(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		stackDisabled  bool
		moduleDisabled bool
	}{
		{name: "stack", stackDisabled: true},
		{name: "module", moduleDisabled: true},
		{name: "stack-and-module", stackDisabled: true, moduleDisabled: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, stack, module, delegated := lifecycleFixture(t)
			stack.Spec.Disabled = scenario.stackDisabled
			module.Spec.Disabled = scenario.moduleDisabled

			controller := reconcileConnectivityModule
			require.NoError(t, controller(ctx, stack, module))
			got := newDelegatedConnectivity(stack.Name)
			require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), got))
			require.Equal(t, delegated.GetUID(), got.GetUID())
			require.Equal(t, delegated.GetFinalizers(), got.GetFinalizers())
			require.Equal(t, map[string]any{"disabled": true, "ledgerAddress": "retained", "api": map[string]any{"enabled": true}}, got.Object["spec"])
			require.True(t, credentialsExist(t, ctx, stack.Name))
			require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name))
			require.NoError(t, ctx.client.Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: "managed"}, &corev1.Secret{}))
			require.NoError(t, controller(ctx, stack, module))
			require.NoError(t, unstructured.SetNestedField(got.Object, "Disabled", "status", "phase"))
			require.NoError(t, unstructured.SetNestedField(got.Object, got.GetGeneration()-1, "status", "observedGeneration"))
			require.NoError(t, ctx.client.Update(ctx, got))
			require.NoError(t, controller(ctx, stack, module), "disable does not wait for observed generation")
			require.NoError(t, unstructured.SetNestedField(got.Object, got.GetGeneration(), "status", "observedGeneration"))
			require.NoError(t, ctx.client.Update(ctx, got))
			require.NoError(t, controller(ctx, stack, module))
		})
	}
}

func TestConnectivityClearingOneDisableFlagDoesNotResume(t *testing.T) {
	for _, remaining := range []string{"stack", "module"} {
		t.Run(remaining, func(t *testing.T) {
			ctx, stack, module, delegated := lifecycleFixture(t)
			module.Spec.Disabled = true
			require.NoError(t, reconcileConnectivityModule(ctx, stack, module))
			if remaining == "stack" {
				module.Spec.Disabled = false
			} else {
				stack.Spec.Disabled = false
			}
			require.NoError(t, reconcileConnectivityModule(ctx, stack, module))
			got := newDelegatedConnectivity(stack.Name)
			require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), got))
			disabled, _, err := unstructured.NestedBool(got.Object, "spec", "disabled")
			require.NoError(t, err)
			require.True(t, disabled)
			require.Equal(t, delegated.GetUID(), got.GetUID())
			require.True(t, credentialsExist(t, ctx, stack.Name))
			require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name))
		})
	}
}

func TestDisableConnectivityRejectsForeignOwnershipAndDeletion(t *testing.T) {
	for _, scenario := range []string{"foreign-owner", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, stack, module, delegated := lifecycleFixture(t)
			switch scenario {
			case "foreign-owner":
				delegated.SetOwnerReferences(nil)
				require.NoError(t, ctx.client.Update(ctx, delegated))
			case "deleting":
				require.NoError(t, ctx.client.Delete(ctx, delegated))
			}
			require.True(t, core.IsApplicationError(disableConnectivity(ctx, stack, nil, module)))
			got := newDelegatedConnectivity(stack.Name)
			require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), got))
			disabled, _, err := unstructured.NestedBool(got.Object, "spec", "disabled")
			require.NoError(t, err)
			require.False(t, disabled)
			require.True(t, credentialsExist(t, ctx, stack.Name))
			require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name))
		})
	}
}

func TestDisableConnectivityPatchRejectsConcurrentEditAndReplacement(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent-edit", true: "replacement"}[replace], func(t *testing.T) {
			ctx, stack, module, delegated := lifecycleFixture(t)
			underlying := ctx.client
			ctx.client = interceptor.NewClient(underlying.(client.WithWatch), interceptor.Funcs{Patch: func(c context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				current := newDelegatedConnectivity(stack.Name)
				require.NoError(t, cl.Get(c, client.ObjectKeyFromObject(delegated), current))
				if replace {
					current.SetFinalizers(nil)
					require.NoError(t, cl.Update(c, current))
					require.NoError(t, cl.Delete(c, current))
					current.SetResourceVersion("")
					current.SetUID(types.UID("replacement"))
					require.NoError(t, cl.Create(c, current))
				} else {
					require.NoError(t, unstructured.SetNestedField(current.Object, "edited", "spec", "ledgerAddress"))
					require.NoError(t, cl.Update(c, current))
				}
				return cl.Patch(c, obj, patch, opts...)
			}})
			require.Error(t, disableConnectivity(ctx, stack, nil, module))
			got := newDelegatedConnectivity(stack.Name)
			require.NoError(t, underlying.Get(ctx, client.ObjectKeyFromObject(delegated), got))
			disabled, _, err := unstructured.NestedBool(got.Object, "spec", "disabled")
			require.NoError(t, err)
			require.False(t, disabled)
		})
	}
}

func TestFinalizeConnectivityRequestsDeletionWithoutWaiting(t *testing.T) {
	ctx, stack, module, delegated := lifecycleFixture(t)
	require.NoError(t, finalizeConnectivity(ctx, module))
	got := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), got))
	require.False(t, got.GetDeletionTimestamp().IsZero())
	require.Contains(t, got.GetFinalizers(), connectivityLifecycleFinalizer)
	require.False(t, credentialsExist(t, ctx, stack.Name))
	require.NoError(t, finalizeConnectivity(ctx, module), "repeated request does not wait for the delegated finalizer")
}

func TestFinalizeConnectivityDoesNotWaitForLifecycleFinalizerInstallation(t *testing.T) {
	ctx, _, module, delegated := lifecycleFixture(t)
	delegated.SetFinalizers(nil)
	require.NoError(t, ctx.client.Update(ctx, delegated))
	require.NoError(t, finalizeConnectivity(ctx, module))
	require.True(t, apierrors.IsNotFound(ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), delegated)))
}

func TestFinalizeConnectivityToleratesAbsentDelegatedAPI(t *testing.T) {
	ctx, stack, module, _ := lifecycleFixture(t)
	ctx.client = interceptor.NewClient(ctx.client.(client.WithWatch), interceptor.Funcs{
		Get: func(c context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if obj.GetObjectKind().GroupVersionKind() == connectivityGVK {
				return &apimeta.NoKindMatchError{GroupKind: connectivityGVK.GroupKind()}
			}
			return cl.Get(c, key, obj, opts...)
		},
	})
	require.NoError(t, finalizeConnectivity(ctx, module))
	require.False(t, credentialsExist(t, ctx, stack.Name))
	require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name))
}

func TestFinalizeConnectivityPreservesCredentialsOnUnprovenCleanup(t *testing.T) {
	for _, scenario := range []string{"forbidden-read", "failed-read", "failed-delete", "foreign-owner"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, stack, module, delegated := lifecycleFixture(t)
			switch scenario {
			case "foreign-owner":
				delegated.SetOwnerReferences(nil)
				require.NoError(t, ctx.client.Update(ctx, delegated))
			default:
				ctx.client = interceptor.NewClient(ctx.client.(client.WithWatch), interceptor.Funcs{
					Get: func(c context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if obj.GetObjectKind().GroupVersionKind() == connectivityGVK {
							if scenario == "failed-read" {
								return errors.New("discovery unavailable")
							}
							if scenario == "forbidden-read" {
								return apierrors.NewForbidden(schema.GroupResource{Group: connectivityGVK.Group, Resource: "connectivities"}, key.Name, errors.New("denied"))
							}
						}
						return cl.Get(c, key, obj, opts...)
					}, Delete: func(c context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						if scenario == "failed-delete" && obj.GetObjectKind().GroupVersionKind() == connectivityGVK {
							return errors.New("delete failed")
						}
						return cl.Delete(c, obj, opts...)
					},
				})
			}
			require.Error(t, finalizeConnectivity(ctx, module))
			require.True(t, credentialsExist(t, ctx, stack.Name))
		})
	}
}

func setDelegatedReadyCondition(object *unstructured.Unstructured, status metav1.ConditionStatus, generation int64) {
	_ = unstructured.SetNestedSlice(object.Object, []any{map[string]any{
		"type": "Ready", "status": string(status), "observedGeneration": generation,
	}}, "status", "conditions")
}

func TestConnectivityResourceReadyRequiresCurrentTrueCondition(t *testing.T) {
	for _, scenario := range []string{"absent", "stale", "false", "unknown", "malformed", "disabled", "deleting", "ready"} {
		t.Run(scenario, func(t *testing.T) {
			object := newDelegatedConnectivity("stack0")
			object.SetGeneration(9)
			require.NoError(t, unstructured.SetNestedField(object.Object, "Ready", "status", "phase"))
			// Legacy global generation and phase cannot grant readiness.
			require.NoError(t, unstructured.SetNestedField(object.Object, int64(9), "status", "observedGeneration"))
			if scenario != "absent" {
				setDelegatedReadyCondition(object, metav1.ConditionTrue, 9)
			}
			switch scenario {
			case "stale":
				setDelegatedReadyCondition(object, metav1.ConditionTrue, 8)
			case "false":
				setDelegatedReadyCondition(object, metav1.ConditionFalse, 9)
			case "unknown":
				setDelegatedReadyCondition(object, metav1.ConditionUnknown, 9)
			case "malformed":
				require.NoError(t, unstructured.SetNestedField(object.Object, "invalid", "status", "conditions"))
			case "disabled":
				require.NoError(t, unstructured.SetNestedField(object.Object, true, "spec", "disabled"))
			case "deleting":
				timestamp := metav1.Now()
				object.SetDeletionTimestamp(&timestamp)
			}
			ready, _ := connectivityResourceReady(object)
			require.Equal(t, scenario == "ready", ready)
		})
	}
}

func TestConnectivityReenableClearsDisabledAndWaitsForCurrentReadyGeneration(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	ledger, credentials := newReadyLedgerPrerequisites()
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: "stack-uid"}}
	module := newConnectivity("stack0", stack.Name)
	module.UID = "module-uid"
	module.Spec.Version = "v1.0.0"
	ctx := newReconcileTestContext(t, stack, module, ledger, credentials)
	require.True(t, core.IsApplicationError(reconcileConnectivityModule(ctx, stack, module)))
	delegated := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), delegated))
	delegated.SetUID("delegated-uid")
	delegated.SetGeneration(5)
	require.NoError(t, unstructured.SetNestedField(delegated.Object, true, "spec", "disabled"))
	require.NoError(t, unstructured.SetNestedField(delegated.Object, "Disabled", "status", "phase"))
	setDelegatedReadyCondition(delegated, metav1.ConditionFalse, 5)
	require.NoError(t, ctx.client.Update(ctx, delegated))
	require.True(t, core.IsApplicationError(reconcileConnectivityModule(ctx, stack, module)))
	got := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), got))
	disabled, _, err := unstructured.NestedBool(got.Object, "spec", "disabled")
	require.NoError(t, err)
	require.False(t, disabled)
	require.Equal(t, delegated.GetUID(), got.GetUID())
	require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name))
	require.NoError(t, unstructured.SetNestedField(got.Object, "Ready", "status", "phase"))
	setDelegatedReadyCondition(got, metav1.ConditionTrue, 4)
	require.NoError(t, ctx.client.Update(ctx, got))
	require.True(t, core.IsApplicationError(reconcileConnectivityModule(ctx, stack, module)))
	require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name), "stale Ready must not reopen exposure")
}

func TestConnectivityAPIReadinessRejectsAuthoritativeLifecycleChange(t *testing.T) {
	for _, scenario := range []string{"stale-generation", "pending", "disabled", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, stack, module, delegated := lifecycleFixture(t)
			require.NoError(t, unstructured.SetNestedField(delegated.Object, "Ready", "status", "phase"))
			setDelegatedReadyCondition(delegated, metav1.ConditionTrue, delegated.GetGeneration())
			// The cache still reports a Ready snapshot; the API reader sees a newer state.
			ready, _ := connectivityResourceReady(delegated)
			require.True(t, ready)
			direct := delegated.DeepCopy()
			switch scenario {
			case "stale-generation":
				direct.SetGeneration(direct.GetGeneration() + 1)
			case "pending":
				setDelegatedReadyCondition(direct, metav1.ConditionFalse, direct.GetGeneration())
			case "disabled":
				require.NoError(t, unstructured.SetNestedField(direct.Object, true, "spec", "disabled"))
			case "deleting":
				timestamp := metav1.Now()
				direct.SetDeletionTimestamp(&timestamp)
			}
			directClient := interceptor.NewClient(ctx.client.(client.WithWatch), interceptor.Funcs{Get: func(c context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if obj.GetObjectKind().GroupVersionKind() == connectivityGVK {
					direct.DeepCopyInto(obj.(*unstructured.Unstructured))
					return nil
				}
				t.Fatal("runtime rollout must not be read after lifecycle gate closed")
				return nil
			}})
			ctx.apiReader = directClient
			ready, message, err := connectivityAPIAuthRolloutReady(ctx, stack.Name, module, nil)
			require.NoError(t, err)
			require.False(t, ready)
			require.NotEmpty(t, message)
		})
	}
}

func TestFinalizeConnectivityDoesNotUseCachedAbsence(t *testing.T) {
	ctx, stack, module, delegated := lifecycleFixture(t)
	ctx.apiReader = ctx.client
	ctx.client = interceptor.NewClient(ctx.client.(client.WithWatch), interceptor.Funcs{Get: func(c context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		if obj.GetObjectKind().GroupVersionKind() == connectivityGVK {
			return apierrors.NewNotFound(schema.GroupResource{Group: connectivityGVK.Group, Resource: "connectivities"}, key.Name)
		}
		return cl.Get(c, key, obj, opts...)
	}})
	require.NoError(t, finalizeConnectivity(ctx, module))
	got := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.apiReader.Get(ctx, client.ObjectKeyFromObject(delegated), got))
	require.False(t, got.GetDeletionTimestamp().IsZero())
	require.False(t, credentialsExist(t, ctx, stack.Name))
}

func TestConnectivityRestoreDoesNotMutateDeletingResource(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	ledger, credentials := newReadyLedgerPrerequisites()
	ctx, stack, module, delegated := lifecycleFixture(t)
	stack.Spec.Disabled = false
	require.NoError(t, ctx.client.Create(ctx, ledger))
	// Use the prerequisite credential's ready status while preserving fixture ownership.
	existing := newLedgerCredentialsForStack(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(existing), existing))
	existing.Object["status"] = credentials.Object["status"]
	require.NoError(t, ctx.client.Update(ctx, existing))
	require.NoError(t, ctx.client.Delete(ctx, delegated))
	before := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), before))
	require.Error(t, Reconcile(ctx, stack, module, "v1.0.0"))
	after := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), after))
	require.Equal(t, before.GetUID(), after.GetUID())
	require.Equal(t, before.Object["spec"], after.Object["spec"])
	require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name))
	_, err := updateExistingConnectivityAPIAuth(ctx, stack.Name, module, nil)
	require.Error(t, err)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), after))
	require.Equal(t, before.Object["spec"], after.Object["spec"])
}

func TestConnectivityModuleDisabledAddsStackOwnerWithoutLedger(t *testing.T) {
	ctx, stack, module, delegated := lifecycleFixture(t)
	require.NoError(t, reconcileConnectivityModule(ctx, stack, module))
	got := &v1beta1.Connectivity{}
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(module), got))
	require.True(t, metav1.IsControlledBy(delegated, module))
	found := false
	for _, owner := range got.OwnerReferences {
		if owner.UID == stack.UID && owner.Kind == "Stack" {
			found = true
		}
	}
	require.True(t, found, "a module created while disabled must still belong to its Stack")
}

func TestConnectivityModuleEnabledRetainsLedgerRequirement(t *testing.T) {
	ctx, stack, module, delegated := lifecycleFixture(t)
	stack.Spec.Disabled = false
	module.Spec.Version = "v1.0.0"
	err := reconcileConnectivityModule(ctx, stack, module)
	require.True(t, core.IsApplicationError(err), "%v", err)
	condition := module.GetConditions().Get(core.DependenciesSatisfiedCondition)
	require.NotNil(t, condition)
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	// Missing Ledger takes the existing prerequisite cleanup path.
	require.False(t, credentialsExist(t, ctx, stack.Name))
	got := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), got))
	require.False(t, got.GetDeletionTimestamp().IsZero())
}

func TestConnectivityModuleKeepsDependencyAndVersionWatches(t *testing.T) {
	options := &core.ReconcilerOptions[*v1beta1.Connectivity]{
		Owns:     map[client.Object][]builder.OwnsOption{},
		Watchers: map[client.Object]core.ReconcilerOptionsWatch{},
	}
	for _, option := range connectivityReconcilerOptions() {
		option(options)
	}
	for _, target := range []client.Object{&v1beta1.Ledger{}, &v1beta1.Versions{}, &v1beta1.Auth{}, &v1beta1.Gateway{}} {
		found := false
		for object := range options.Watchers {
			if reflect.TypeOf(object) == reflect.TypeOf(target) {
				found = true
			}
		}
		require.True(t, found, "missing watch for %T", target)
	}
}
