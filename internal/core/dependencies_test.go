package core_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestStackDependencyLookups(t *testing.T) {
	t.Parallel()

	auth := unstructuredDependency("Auth", "auth0", "stack0")
	other := unstructuredDependency("Auth", "auth1", "stack1")
	ctx := testutil.NewContext(auth, other)

	var auths []*v1beta1.Auth
	require.NoError(t, core.GetAllStackDependencies(ctx, "stack0", &auths))
	require.Len(t, auths, 1)
	require.Equal(t, "auth0", auths[0].Name)
	require.Equal(t, "stack0", auths[0].Spec.Stack)

	single := &v1beta1.Auth{}
	require.NoError(t, core.GetSingleDependency(ctx, "stack0", single))
	require.Equal(t, "auth0", single.Name)

	found, err := core.HasDependency(ctx, "stack0", &v1beta1.Auth{})
	require.NoError(t, err)
	require.True(t, found)

	found, err = core.GetIfExists(ctx, "missing", &v1beta1.Auth{})
	require.NoError(t, err)
	require.False(t, found)
}

func TestStackDependencyLookupErrors(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		unstructuredDependency("Auth", "auth0", "stack0"),
		unstructuredDependency("Auth", "auth1", "stack0"),
	)

	require.True(t, errors.Is(core.GetSingleDependency(ctx, "missing", &v1beta1.Auth{}), core.ErrNotFound))
	require.True(t, errors.Is(core.GetSingleDependency(ctx, "stack0", &v1beta1.Auth{}), core.ErrMultipleInstancesFound))

	found, err := core.HasDependency(ctx, "stack0", &v1beta1.Auth{})
	require.NoError(t, err)
	require.True(t, found)

	found, err = core.GetIfExists(ctx, "stack0", &v1beta1.Auth{})
	require.ErrorIs(t, err, core.ErrMultipleInstancesFound)
	require.False(t, found)
}

func TestBuildAndMapReconcileRequests(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		unstructuredDependency("Auth", "auth0", "stack0"),
		unstructuredDependency("Auth", "auth1", "stack1"),
	)

	requests := core.BuildReconcileRequests(context.Background(), ctx.GetClient(), ctx.GetScheme(), &v1beta1.Auth{}, client.MatchingFields{"stack": "stack0"})
	require.Equal(t, []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: "auth0"},
	}}, requests)

	require.Equal(t, []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: "manual", Namespace: "ns"},
	}}, core.MapObjectToReconcileRequests(&v1beta1.Auth{
		ObjectMeta: withNamespace(testutil.ObjectMeta("manual"), "ns"),
	}))
}

func TestWatchDependentsAndStack(t *testing.T) {
	t.Parallel()

	auth := unstructuredDependency("Auth", "auth0", "stack0")
	ctx := testutil.NewContext(auth)
	mgr := &testutil.Manager{Client: ctx.GetClient(), Scheme: ctx.GetScheme()}

	dependentWatcher := core.WatchDependents(mgr, &v1beta1.Auth{})
	requests := dependentWatcher(context.Background(), &v1beta1.Database{Spec: v1beta1.DatabaseSpec{
		StackDependency: v1beta1.StackDependency{Stack: "stack0"},
	}})
	require.Equal(t, []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: "auth0"},
	}}, requests)

	stackWatcher := core.Watch(mgr, &v1beta1.Auth{})
	requests = stackWatcher(context.Background(), &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")})
	require.Equal(t, []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: "auth0"},
	}}, requests)
}

func TestForObjectControllerStatusHandling(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		object := &v1beta1.Auth{}
		controller := core.ForObjectController(func(core.Context, *core.ReconcilerOptions[*v1beta1.Auth], *v1beta1.Auth) error {
			return nil
		})

		err := controller(testutil.NewContext(), &core.ReconcilerOptions[*v1beta1.Auth]{}, object)
		require.NoError(t, err)
		require.True(t, object.IsReady())
		require.Equal(t, "Up to date", object.Status.Info)
	})

	t.Run("application error updates status without requeue error", func(t *testing.T) {
		object := &v1beta1.Auth{}
		controller := core.ForObjectController(func(core.Context, *core.ReconcilerOptions[*v1beta1.Auth], *v1beta1.Auth) error {
			return core.NewPendingError().WithMessage("waiting")
		})

		err := controller(testutil.NewContext(), &core.ReconcilerOptions[*v1beta1.Auth]{}, object)
		require.NoError(t, err)
		require.False(t, object.IsReady())
		require.Equal(t, "waiting", object.Status.Info)
	})

	t.Run("regular error bubbles up", func(t *testing.T) {
		object := &v1beta1.Auth{}
		controller := core.ForObjectController(func(core.Context, *core.ReconcilerOptions[*v1beta1.Auth], *v1beta1.Auth) error {
			return fmt.Errorf("boom")
		})

		err := controller(testutil.NewContext(), &core.ReconcilerOptions[*v1beta1.Auth]{}, object)
		require.EqualError(t, err, "boom")
		require.False(t, object.IsReady())
		require.Equal(t, "boom", object.Status.Info)
	})

	t.Run("pending observed condition marks object not ready", func(t *testing.T) {
		object := &v1beta1.Auth{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
		object.GetConditions().AppendOrReplace(v1beta1.Condition{
			Type:               "DatabaseReady",
			Status:             metav1.ConditionFalse,
			Reason:             "Pending",
			ObservedGeneration: 7,
		}, v1beta1.ConditionTypeMatch("DatabaseReady"))
		controller := core.ForObjectController(func(core.Context, *core.ReconcilerOptions[*v1beta1.Auth], *v1beta1.Auth) error {
			return nil
		})

		err := controller(testutil.NewContext(), &core.ReconcilerOptions[*v1beta1.Auth]{}, object)
		require.NoError(t, err)
		require.False(t, object.IsReady())
		require.Equal(t, "pending condition: DatabaseReady/Pending", object.Status.Info)
	})
}

func TestForStackDependency(t *testing.T) {
	t.Parallel()

	t.Run("missing stack returns application error", func(t *testing.T) {
		object := &v1beta1.Auth{Spec: v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "missing"}}}
		controller := core.ForStackDependency(func(core.Context, *v1beta1.Stack, *core.ReconcilerOptions[*v1beta1.Auth], *v1beta1.Auth) error {
			t.Fatal("controller should not be called")
			return nil
		}, false)

		err := controller(testutil.NewContext(), &core.ReconcilerOptions[*v1beta1.Auth]{}, object)
		require.True(t, core.IsApplicationError(err))
		require.EqualError(t, err, "stack not found")
	})

	t.Run("skipped stack records reconciled condition", func(t *testing.T) {
		stack := &v1beta1.Stack{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "stack0",
				Generation: 3,
				Annotations: map[string]string{
					v1beta1.SkipLabel: "true",
				},
			},
		}
		object := &v1beta1.Auth{Spec: v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}}}
		controller := core.ForStackDependency(func(core.Context, *v1beta1.Stack, *core.ReconcilerOptions[*v1beta1.Auth], *v1beta1.Auth) error {
			t.Fatal("controller should not be called")
			return nil
		}, false)

		err := controller(testutil.NewContext(stack), &core.ReconcilerOptions[*v1beta1.Auth]{}, object)
		require.NoError(t, err)
		condition := object.GetConditions().Get("ReconciledWithStack")
		require.NotNil(t, condition)
		require.Equal(t, "Skipped", condition.Reason)
		require.Equal(t, int64(3), condition.ObservedGeneration)
	})

	t.Run("deleted stack is rejected unless allowed", func(t *testing.T) {
		now := metav1.Now()
		stack := &v1beta1.Stack{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "stack0",
				DeletionTimestamp: &now,
				Finalizers:        []string{"keep"},
			},
		}
		object := &v1beta1.Auth{Spec: v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}}}
		called := false
		controller := core.ForStackDependency(func(core.Context, *v1beta1.Stack, *core.ReconcilerOptions[*v1beta1.Auth], *v1beta1.Auth) error {
			called = true
			return nil
		}, false)

		err := controller(testutil.NewContext(stack), &core.ReconcilerOptions[*v1beta1.Auth]{}, object)
		require.True(t, core.IsApplicationError(err))
		require.False(t, called)

		allowDeletedController := core.ForStackDependency(func(core.Context, *v1beta1.Stack, *core.ReconcilerOptions[*v1beta1.Auth], *v1beta1.Auth) error {
			called = true
			return nil
		}, true)
		err = allowDeletedController(testutil.NewContext(stack), &core.ReconcilerOptions[*v1beta1.Auth]{}, object)
		require.NoError(t, err)
		require.True(t, called)
	})
}

func TestReconcilerOptions(t *testing.T) {
	t.Parallel()

	options := core.ReconcilerOptions[*v1beta1.Auth]{
		Owns:     map[client.Object][]builder.OwnsOption{},
		Watchers: map[client.Object]core.ReconcilerOptionsWatch{},
	}

	rawCalled := false
	core.WithOwn[*v1beta1.Auth](&appsv1.Deployment{})(&options)
	core.WithRaw[*v1beta1.Auth](func(core.Context, *builder.Builder) error {
		rawCalled = true
		return nil
	})(&options)
	core.WithFinalizer[*v1beta1.Auth]("cleanup", func(core.Context, *v1beta1.Auth) error {
		return nil
	})(&options)
	core.WithWatchSettings[*v1beta1.Auth]()(&options)
	core.WithWatchDependency[*v1beta1.Auth](&v1beta1.Database{})(&options)
	core.WithWatchStack[*v1beta1.Auth]()(&options)
	core.WithWatch[*v1beta1.Auth](func(core.Context, *v1beta1.Database) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "auth0"}}}
	})(&options)

	require.Len(t, options.Owns, 1)
	require.Len(t, options.Raws, 1)
	require.Len(t, options.Finalizers, 1)
	require.Len(t, options.Watchers, 4)
	require.NoError(t, options.Raws[0](testutil.NewContext(), nil))
	require.True(t, rawCalled)

	ctx := testutil.NewContext(unstructuredDependency("Auth", "auth0", "stack0"))
	mgr := &testutil.Manager{Client: ctx.GetClient(), Scheme: ctx.GetScheme()}
	for watched, watcher := range options.Watchers {
		handler, watchOptions := watcher.Handler(mgr, nil, &v1beta1.Auth{})
		require.NotNil(t, handler)
		if _, ok := watched.(*v1beta1.Stack); ok {
			require.Len(t, watchOptions, 1)
		}
	}
}

func TestGetModuleVersion(t *testing.T) {
	t.Parallel()

	t.Run("module version wins", func(t *testing.T) {
		ctx := testutil.NewContext()
		stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0"), Spec: v1beta1.StackSpec{Version: "v-stack"}}
		module := &v1beta1.Auth{Spec: v1beta1.AuthSpec{ModuleProperties: v1beta1.ModuleProperties{Version: "v-module"}}}

		version, err := core.GetModuleVersion(ctx, stack, module)
		require.NoError(t, err)
		require.Equal(t, "v-module", version)
	})

	t.Run("stack version is fallback", func(t *testing.T) {
		ctx := testutil.NewContext()
		stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0"), Spec: v1beta1.StackSpec{Version: "v-stack"}}

		version, err := core.GetModuleVersion(ctx, stack, &v1beta1.Auth{})
		require.NoError(t, err)
		require.Equal(t, "v-stack", version)
	})

	t.Run("versions file resolves by lower-case kind", func(t *testing.T) {
		ctx := testutil.NewContext(&v1beta1.Versions{
			ObjectMeta: testutil.ObjectMeta("versions0"),
			Spec: map[string]string{
				"auth":   "v-auth",
				"ledger": "v-ledger",
			},
		})
		stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0"), Spec: v1beta1.StackSpec{VersionsFromFile: "versions0"}}

		version, err := core.GetModuleVersion(ctx, stack, &v1beta1.Auth{})
		require.NoError(t, err)
		require.Equal(t, "v-auth", version)
	})

	t.Run("missing versions file value returns explicit no version error", func(t *testing.T) {
		ctx := testutil.NewContext(&v1beta1.Versions{
			ObjectMeta: testutil.ObjectMeta("versions0"),
			Spec:       map[string]string{"ledger": "v-ledger"},
		})
		stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0"), Spec: v1beta1.StackSpec{VersionsFromFile: "versions0"}}

		_, err := core.GetModuleVersion(ctx, stack, &v1beta1.Auth{})
		require.ErrorIs(t, err, core.ErrNoVersionFound)
		require.Contains(t, err.Error(), "module not found in Versions resource versions0")
	})

	t.Run("no source returns explicit no version error", func(t *testing.T) {
		_, err := core.GetModuleVersion(testutil.NewContext(), &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}, &v1beta1.Auth{})
		require.ErrorIs(t, err, core.ErrNoVersionFound)
		require.Contains(t, err.Error(), "stack must define spec.version")
	})
}

func TestVersionComparisons(t *testing.T) {
	t.Parallel()

	require.True(t, core.IsGreaterOrEqual("v2.0.0", "v1.9.0"))
	require.True(t, core.IsGreaterOrEqual("latest", "v1.9.0"))
	require.False(t, core.IsGreaterOrEqual("v1.8.0", "v1.9.0"))
	require.False(t, core.IsGreaterOrEqual("v1.8.0", "latest"))
	require.True(t, core.IsGreaterOrEqual("latest", "latest"))

	require.True(t, core.IsLower("v1.8.0", "v1.9.0"))
	require.True(t, core.IsLower("v1.8.0", "latest"))
	require.False(t, core.IsLower("latest", "v1.9.0"))
	require.False(t, core.IsLower("v2.0.0", "v1.9.0"))
}

func TestForModule(t *testing.T) {
	t.Parallel()

	t.Run("adds stack owner reference and calls controller with resolved version", func(t *testing.T) {
		stack := &v1beta1.Stack{
			ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid"), Generation: 4},
			Spec:       v1beta1.StackSpec{Version: "v-stack"},
		}
		module := &v1beta1.Auth{
			ObjectMeta: metav1.ObjectMeta{Name: "auth0", UID: types.UID("auth-uid")},
			Spec:       v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
		}
		ctx := testutil.NewContext(stack, module)
		var gotVersion string
		called := false
		controller := core.ForModule(func(ctx core.Context, stack *v1beta1.Stack, options *core.ReconcilerOptions[*v1beta1.Auth], req *v1beta1.Auth, version string) error {
			called = true
			gotVersion = version
			return nil
		})

		err := controller(ctx, stack, &core.ReconcilerOptions[*v1beta1.Auth]{Owns: map[client.Object][]builder.OwnsOption{}}, module)
		require.NoError(t, err)
		require.True(t, called)
		require.Equal(t, "v-stack", gotVersion)
		require.True(t, hasOwnerReferenceNamed(module.OwnerReferences, "Stack", "stack0"))
		require.Equal(t, "Spec", module.GetConditions().Get("ReconciledWithStack").Reason)

		persisted := &v1beta1.Auth{}
		require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "auth0"}, persisted))
		require.True(t, hasOwnerReferenceNamed(persisted.OwnerReferences, "Stack", "stack0"))
	})

	t.Run("disabled stack deletes controlled owned objects and skips resources", func(t *testing.T) {
		stack := &v1beta1.Stack{
			ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid"), Generation: 5},
			Spec:       v1beta1.StackSpec{Version: "v-stack", Disabled: true},
		}
		module := &v1beta1.Auth{
			ObjectMeta: metav1.ObjectMeta{Name: "auth0", UID: types.UID("auth-uid")},
			Spec:       v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
		}
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "owned", Namespace: "stack0"}}
		require.NoError(t, controllerutil.SetControllerReference(module, deployment, testutil.NewScheme()))
		database := &v1beta1.Database{
			ObjectMeta: metav1.ObjectMeta{Name: "database0"},
			Spec:       v1beta1.DatabaseSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
		}
		ctx := testutil.NewContext(stack, module, deployment, database)
		called := false
		controller := core.ForModule(func(ctx core.Context, stack *v1beta1.Stack, options *core.ReconcilerOptions[*v1beta1.Auth], req *v1beta1.Auth, version string) error {
			called = true
			return nil
		})

		err := controller(ctx, stack, &core.ReconcilerOptions[*v1beta1.Auth]{
			Owns: map[client.Object][]builder.OwnsOption{
				&appsv1.Deployment{}: {},
				&v1beta1.Database{}:  {},
			},
		}, module)
		require.NoError(t, err)
		require.False(t, called)
		require.Equal(t, "Spec", module.GetConditions().Get("ReconciledWithStack").Reason)

		require.Error(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "owned", Namespace: "stack0"}, &appsv1.Deployment{}))
		require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "database0"}, &v1beta1.Database{}))
	})

	t.Run("underlying controller error stops reconciled condition", func(t *testing.T) {
		stack := &v1beta1.Stack{
			ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")},
			Spec:       v1beta1.StackSpec{Version: "v-stack"},
		}
		module := &v1beta1.Auth{
			ObjectMeta: metav1.ObjectMeta{Name: "auth0", UID: types.UID("auth-uid")},
			Spec:       v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
		}
		ctx := testutil.NewContext(stack, module)
		controller := core.ForModule(func(ctx core.Context, stack *v1beta1.Stack, options *core.ReconcilerOptions[*v1beta1.Auth], req *v1beta1.Auth, version string) error {
			return fmt.Errorf("module failed")
		})

		err := controller(ctx, stack, &core.ReconcilerOptions[*v1beta1.Auth]{Owns: map[client.Object][]builder.OwnsOption{}}, module)
		require.EqualError(t, err, "module failed")
		require.Nil(t, module.GetConditions().Get("ReconciledWithStack"))
	})
}

func unstructuredDependency(kind, name, stack string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "formance.com/v1beta1",
		"kind":       kind,
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"stack": stack,
		},
	}}
	return object
}

func withNamespace(meta metav1.ObjectMeta, namespace string) metav1.ObjectMeta {
	meta.Namespace = namespace
	return meta
}

func hasOwnerReferenceNamed(references []metav1.OwnerReference, kind, name string) bool {
	for _, reference := range references {
		if reference.Kind == kind && reference.Name == name {
			return true
		}
	}
	return false
}
