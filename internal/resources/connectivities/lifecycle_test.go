package connectivities

import (
	"context"
	"errors"
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
	ctx, stack, module, delegated := lifecycleFixture(t)
	opts := &core.ReconcilerOptions[*v1beta1.Connectivity]{Owns: map[client.Object][]builder.OwnsOption{delegated: nil}}
	controller := core.ForModule(core.NoRequirements(), disableConnectivity, func(core.Context, *v1beta1.Stack, *core.ReconcilerOptions[*v1beta1.Connectivity], *v1beta1.Connectivity, string) error {
		t.Fatal("normal controller ran")
		return nil
	})
	require.NoError(t, controller(ctx, stack, opts, module))
	got := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), got))
	require.Equal(t, delegated.GetUID(), got.GetUID())
	require.Equal(t, delegated.GetFinalizers(), got.GetFinalizers())
	require.Equal(t, map[string]any{"disabled": true, "ledgerAddress": "retained", "api": map[string]any{"enabled": true}}, got.Object["spec"])
	require.True(t, credentialsExist(t, ctx, stack.Name))
	require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name))
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: "managed"}, &corev1.Secret{}))
	require.NoError(t, controller(ctx, stack, opts, module))
	require.NoError(t, unstructured.SetNestedField(got.Object, "Disabled", "status", "phase"))
	require.NoError(t, unstructured.SetNestedField(got.Object, got.GetGeneration()-1, "status", "observedGeneration"))
	require.NoError(t, ctx.client.Update(ctx, got))
	require.NoError(t, controller(ctx, stack, opts, module), "disable does not wait for observed generation")
	require.NoError(t, unstructured.SetNestedField(got.Object, got.GetGeneration(), "status", "observedGeneration"))
	require.NoError(t, ctx.client.Update(ctx, got))
	require.NoError(t, controller(ctx, stack, opts, module))
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

func TestFinalizeConnectivityPreservesCredentialsOnUnprovenCleanup(t *testing.T) {
	for _, scenario := range []string{"forbidden-read", "missing-api", "failed-delete", "foreign-owner"} {
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
							if scenario == "missing-api" {
								return &apimeta.NoKindMatchError{GroupKind: connectivityGVK.GroupKind()}
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

func TestConnectivityResourceReadyRequiresObservedEnabledGeneration(t *testing.T) {
	obj := newDelegatedConnectivity("stack0")
	obj.SetGeneration(9)
	require.NoError(t, unstructured.SetNestedField(obj.Object, "Ready", "status", "phase"))
	ready, _ := connectivityResourceReady(obj)
	require.False(t, ready)
	require.NoError(t, unstructured.SetNestedField(obj.Object, int64(8), "status", "observedGeneration"))
	ready, _ = connectivityResourceReady(obj)
	require.False(t, ready)
	require.NoError(t, unstructured.SetNestedField(obj.Object, int64(9), "status", "observedGeneration"))
	ready, _ = connectivityResourceReady(obj)
	require.True(t, ready)
	require.NoError(t, unstructured.SetNestedField(obj.Object, "Disabled", "status", "phase"))
	ready, _ = connectivityResourceReady(obj)
	require.False(t, ready)
}

func TestConnectivityReenableClearsDisabledAndWaitsForCurrentReadyGeneration(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	ledger, credentials := newReadyLedgerPrerequisites()
	ctx := newReconcileTestContext(t, ledger, credentials)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: "stack-uid"}}
	module := newConnectivity("stack0", stack.Name)
	module.UID = "module-uid"
	require.True(t, core.IsApplicationError(Reconcile(ctx, stack, module, "v1.0.0")))
	delegated := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), delegated))
	delegated.SetUID("delegated-uid")
	delegated.SetGeneration(5)
	require.NoError(t, unstructured.SetNestedField(delegated.Object, true, "spec", "disabled"))
	require.NoError(t, unstructured.SetNestedField(delegated.Object, "Disabled", "status", "phase"))
	require.NoError(t, unstructured.SetNestedField(delegated.Object, int64(5), "status", "observedGeneration"))
	require.NoError(t, ctx.client.Update(ctx, delegated))
	require.True(t, core.IsApplicationError(Reconcile(ctx, stack, module, "v1.0.0")))
	got := newDelegatedConnectivity(stack.Name)
	require.NoError(t, ctx.client.Get(ctx, client.ObjectKeyFromObject(delegated), got))
	disabled, _, err := unstructured.NestedBool(got.Object, "spec", "disabled")
	require.NoError(t, err)
	require.False(t, disabled)
	require.Equal(t, delegated.GetUID(), got.GetUID())
	require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name))
	require.NoError(t, unstructured.SetNestedField(got.Object, "Ready", "status", "phase"))
	require.NoError(t, unstructured.SetNestedField(got.Object, int64(4), "status", "observedGeneration"))
	require.NoError(t, ctx.client.Update(ctx, got))
	require.True(t, core.IsApplicationError(Reconcile(ctx, stack, module, "v1.0.0")))
	require.False(t, gatewayHTTPAPIExists(t, ctx, stack.Name), "stale Ready must not reopen exposure")
}

func TestConnectivityAPIReadinessRejectsAuthoritativeLifecycleChange(t *testing.T) {
	for _, scenario := range []string{"stale-generation", "pending", "disabled", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, stack, module, delegated := lifecycleFixture(t)
			require.NoError(t, unstructured.SetNestedField(delegated.Object, "Ready", "status", "phase"))
			require.NoError(t, unstructured.SetNestedField(delegated.Object, delegated.GetGeneration(), "status", "observedGeneration"))
			// The cache still reports a Ready snapshot; the API reader sees a newer state.
			ready, _ := connectivityResourceReady(delegated)
			require.True(t, ready)
			direct := delegated.DeepCopy()
			switch scenario {
			case "stale-generation":
				direct.SetGeneration(direct.GetGeneration() + 1)
			case "pending":
				require.NoError(t, unstructured.SetNestedField(direct.Object, "Pending", "status", "phase"))
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
