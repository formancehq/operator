package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestModuleRequirementsValidation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))

	tests := []struct {
		name         string
		requirements ModuleRequirements
		wantError    string
	}{
		{
			name:         "explicitly independent",
			requirements: NoRequirements(),
		},
		{
			name:         "valid ready dependency",
			requirements: Requirements(Require(&v1beta1.Broker{}, Ready())),
		},
		{
			name: "valid bounded module version",
			requirements: Requirements(Require(&v1beta1.Ledger{},
				VersionBetween("v2.0.0", "v3.0.0-0"), Ready())),
		},
		{
			name:      "zero value",
			wantError: "must be declared",
		},
		{
			name:         "empty declaration",
			requirements: Requirements(),
			wantError:    "must contain at least one dependency",
		},
		{
			name:         "nil dependency",
			requirements: Requirements(Require((*v1beta1.Ledger)(nil))),
			wantError:    "nil dependency",
		},
		{
			name:         "version constraint on resource",
			requirements: Requirements(Require(&v1beta1.Broker{}, VersionAtLeast("v1.0.0"))),
			wantError:    "does not implement Module",
		},
		{
			name:         "invalid semantic version",
			requirements: Requirements(Require(&v1beta1.Ledger{}, VersionAtLeast("main"))),
			wantError:    "not a valid semantic version",
		},
		{
			name:         "empty version bound",
			requirements: Requirements(Require(&v1beta1.Ledger{}, VersionAtLeast(""))),
			wantError:    "minimum version cannot be empty",
		},
		{
			name: "reversed range",
			requirements: Requirements(Require(&v1beta1.Ledger{},
				VersionBetween("v3.0.0", "v2.0.0"))),
			wantError: "lower minimum than maximum",
		},
		{
			name: "duplicate dependency",
			requirements: Requirements(
				Require(&v1beta1.Ledger{}),
				Require(&v1beta1.Ledger{}, Ready()),
			),
			wantError: "required more than once",
		},
		{
			name: "duplicate lower bound",
			requirements: Requirements(Require(&v1beta1.Ledger{},
				VersionAtLeast("v2.0.0"), VersionAtLeast("v2.1.0"))),
			wantError: "minimum version is declared more than once",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.requirements.validate(scheme)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestEvaluateBrokerRequirement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		brokers    []*v1beta1.Broker
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{
			name:       "missing",
			wantStatus: metav1.ConditionFalse,
			wantReason: dependencyNotFoundReason,
		},
		{
			name: "not ready",
			brokers: []*v1beta1.Broker{
				brokerForRequirementsTest("broker", false),
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: dependencyNotReadyReason,
		},
		{
			name: "ready",
			brokers: []*v1beta1.Broker{
				brokerForRequirementsTest("broker", true),
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: requirementsSatisfiedReason,
		},
		{
			name: "multiple",
			brokers: []*v1beta1.Broker{
				brokerForRequirementsTest("broker-a", true),
				brokerForRequirementsTest("broker-b", true),
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: multipleDependenciesFoundReason,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := make([]client.Object, 0, len(test.brokers))
			for _, broker := range test.brokers {
				objects = append(objects, broker)
			}
			ctx, stack := newRequirementsTestContext(t, objects...)

			evaluation := evaluateModuleRequirements(ctx, stack,
				Requirements(Require(&v1beta1.Broker{}, Ready())))

			require.Equal(t, test.wantStatus, evaluation.status)
			require.Equal(t, test.wantReason, evaluation.reason)
		})
	}
}

func TestEvaluateLedgerEffectiveVersionRequirement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		stackVersion  string
		versions      *v1beta1.Versions
		ledgerVersion string
		requirement   Requirement
		wantStatus    metav1.ConditionStatus
		wantReason    string
	}{
		{
			name:          "module override takes precedence",
			stackVersion:  "v3.1.0",
			ledgerVersion: "v2.9.0",
			requirement:   Require(&v1beta1.Ledger{}, VersionBefore("v3.0.0-0")),
			wantStatus:    metav1.ConditionTrue,
			wantReason:    requirementsSatisfiedReason,
		},
		{
			name:         "Stack version is used",
			stackVersion: "v2.9.0",
			requirement:  Require(&v1beta1.Ledger{}, VersionBefore("v3.0.0-0")),
			wantStatus:   metav1.ConditionTrue,
			wantReason:   requirementsSatisfiedReason,
		},
		{
			name: "Versions entry is used without interpreting the resource name",
			versions: &v1beta1.Versions{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-catalog"},
				Spec:       map[string]string{"ledger": "v2.9.0"},
			},
			requirement: Require(&v1beta1.Ledger{}, VersionBefore("v3.0.0-0")),
			wantStatus:  metav1.ConditionTrue,
			wantReason:  requirementsSatisfiedReason,
		},
		{
			name:          "exclusive upper boundary is rejected",
			ledgerVersion: "v3.0.0-0",
			requirement:   Require(&v1beta1.Ledger{}, VersionBefore("v3.0.0-0")),
			wantStatus:    metav1.ConditionFalse,
			wantReason:    dependencyVersionMismatchReason,
		},
		{
			name:          "inclusive lower boundary is accepted",
			ledgerVersion: "v3.0.0-0",
			requirement:   Require(&v1beta1.Ledger{}, VersionAtLeast("v3.0.0-0")),
			wantStatus:    metav1.ConditionTrue,
			wantReason:    requirementsSatisfiedReason,
		},
		{
			name:          "opaque tag is unknown",
			ledgerVersion: "main",
			requirement:   Require(&v1beta1.Ledger{}, VersionAtLeast("v3.0.0-0")),
			wantStatus:    metav1.ConditionUnknown,
			wantReason:    dependencyVersionNotSemverReason,
		},
		{
			name:        "unresolved effective version is unknown",
			requirement: Require(&v1beta1.Ledger{}, VersionAtLeast("v3.0.0-0")),
			wantStatus:  metav1.ConditionUnknown,
			wantReason:  dependencyVersionUnresolvedReason,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ledger := &v1beta1.Ledger{
				ObjectMeta: metav1.ObjectMeta{Name: "ledger"},
				Spec: v1beta1.LedgerSpec{
					StackDependency:  v1beta1.StackDependency{Stack: "stack"},
					ModuleProperties: v1beta1.ModuleProperties{Version: test.ledgerVersion},
				},
			}
			objects := []client.Object{ledger}
			if test.versions != nil {
				objects = append(objects, test.versions)
			}
			ctx, stack := newRequirementsTestContext(t, objects...)
			stack.Spec.Version = test.stackVersion
			if test.versions != nil {
				stack.Spec.VersionsFromFile = test.versions.Name
			}

			evaluation := evaluateModuleRequirements(ctx, stack, Requirements(test.requirement))

			require.Equal(t, test.wantStatus, evaluation.status)
			require.Equal(t, test.wantReason, evaluation.reason)
		})
	}
}

func TestForModuleGatesReconcilerOnRequirements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		broker      *v1beta1.Broker
		wantCalled  bool
		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantError   bool
		wantCleanup bool
	}{
		{
			name:        "missing dependency blocks reconciler",
			wantStatus:  metav1.ConditionFalse,
			wantReason:  dependencyNotFoundReason,
			wantError:   true,
			wantCleanup: true,
		},
		{
			name:       "satisfied dependency runs reconciler",
			broker:     brokerForRequirementsTest("broker", true),
			wantCalled: true,
			wantStatus: metav1.ConditionTrue,
			wantReason: requirementsSatisfiedReason,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, v1beta1.AddToScheme(scheme))
			stack := &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{Name: "stack", UID: types.UID("stack-uid")},
				Spec:       v1beta1.StackSpec{Version: "v2.2.0"},
			}
			consumer := &v1beta1.Ledger{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ledger",
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: v1beta1.GroupVersion.String(),
						Kind:       "Stack",
						Name:       stack.Name,
						UID:        stack.UID,
					}},
				},
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				},
			}

			objects := []client.Object{stack, consumer}
			if test.broker != nil {
				objects = append(objects, test.broker)
			}
			stackIndex := func(object client.Object) []string {
				if unstructuredObject, ok := object.(*unstructured.Unstructured); ok {
					value, found, err := unstructured.NestedString(unstructuredObject.Object, "spec", "stack")
					if err == nil && found {
						return []string{value}
					}
				}
				return nil
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithIndex(&v1beta1.Broker{}, "stack", stackIndex).
				Build()
			ctx := testContext{
				Context:   context.Background(),
				client:    fakeClient,
				apiReader: fakeClient,
				scheme:    scheme,
			}

			called := false
			controller := ForModule(
				Requirements(Require(&v1beta1.Broker{}, Ready())),
				func(Context, *v1beta1.Stack, *ReconcilerOptions[*v1beta1.Ledger], *v1beta1.Ledger, string) error {
					called = true
					return nil
				},
			)

			cleanupCalled := false
			options := &ReconcilerOptions[*v1beta1.Ledger]{}
			WithUnsatisfiedRequirementsHandler(func(Context, *v1beta1.Stack, *v1beta1.Ledger) error {
				cleanupCalled = true
				return nil
			})(options)
			err := controller(ctx, stack, options, consumer)
			if test.wantError {
				require.Error(t, err)
				require.True(t, IsApplicationError(err))
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantCalled, called)
			require.Equal(t, test.wantCleanup, cleanupCalled)

			condition := consumer.GetConditions().Get(DependenciesSatisfiedCondition)
			require.NotNil(t, condition)
			require.Equal(t, test.wantStatus, condition.Status)
			require.Equal(t, test.wantReason, condition.Reason)
			reconciled := consumer.GetConditions().Get(reconciledWithStackCondition)
			require.NotNil(t, reconciled)
			require.Equal(t, test.wantStatus, reconciled.Status)
		})
	}
}

func TestForModuleDoesNotCleanupUnknownRequirement(t *testing.T) {
	t.Parallel()

	ledger := &v1beta1.Ledger{
		ObjectMeta: metav1.ObjectMeta{Name: "ledger"},
		Spec: v1beta1.LedgerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack"},
		},
	}
	ctx, stack := newRequirementsTestContext(t, ledger)
	consumer := &v1beta1.Connectivity{
		ObjectMeta: metav1.ObjectMeta{
			Name: "connectivity",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: v1beta1.GroupVersion.String(),
				Kind:       "Stack",
				Name:       stack.Name,
			}},
		},
		Spec: v1beta1.ConnectivitySpec{
			StackDependency:  v1beta1.StackDependency{Stack: stack.Name},
			ModuleProperties: v1beta1.ModuleProperties{Version: "v1.0.0"},
		},
	}
	cleanupCalled := false
	options := &ReconcilerOptions[*v1beta1.Connectivity]{}
	WithUnsatisfiedRequirementsHandler(func(Context, *v1beta1.Stack, *v1beta1.Connectivity) error {
		cleanupCalled = true
		return nil
	})(options)
	controller := ForModule(
		Requirements(Require(&v1beta1.Ledger{}, VersionAtLeast("v3.0.0-0"))),
		func(Context, *v1beta1.Stack, *ReconcilerOptions[*v1beta1.Connectivity], *v1beta1.Connectivity, string) error {
			t.Fatal("underlying reconciler must not run for an unknown dependency version")
			return nil
		},
	)

	err := controller(ctx, stack, options, consumer)
	require.Error(t, err)
	require.True(t, IsApplicationError(err))
	require.False(t, cleanupCalled)
	condition := consumer.GetConditions().Get(DependenciesSatisfiedCondition)
	require.NotNil(t, condition)
	require.Equal(t, metav1.ConditionUnknown, condition.Status)
	require.Equal(t, dependencyVersionUnresolvedReason, condition.Reason)
}

func TestWithWatchVersionsRequeuesConsumerForRelevantCatalogEntryChange(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack"},
		Spec:       v1beta1.StackSpec{VersionsFromFile: "catalog"},
	}
	consumer := &v1beta1.Search{
		ObjectMeta: metav1.ObjectMeta{Name: "search"},
		Spec: v1beta1.SearchSpec{
			StackDependency: v1beta1.StackDependency{Stack: stack.Name},
		},
	}
	stackIndex := func(object client.Object) []string {
		return []string{object.(*v1beta1.Stack).Spec.VersionsFromFile}
	}
	consumerIndex := func(object client.Object) []string {
		unstructuredObject := object.(*unstructured.Unstructured)
		value, found, err := unstructured.NestedString(unstructuredObject.Object, "spec", "stack")
		if err == nil && found {
			return []string{value}
		}
		return nil
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(stack, consumer).
		WithIndex(&v1beta1.Stack{}, ".spec.versionsFromFile", stackIndex).
		WithIndex(&v1beta1.Search{}, "stack", consumerIndex).
		Build()
	mgr := reconcileTestManager{client: fakeClient, scheme: scheme}

	options := ReconcilerOptions[*v1beta1.Search]{
		Watchers: map[client.Object]ReconcilerOptionsWatch{},
	}
	WithWatchVersions[*v1beta1.Search](Requirements(
		Require(&v1beta1.Ledger{}, VersionAtLeast("v3.0.0-0")),
	))(&options)
	var versionsWatch ReconcilerOptionsWatch
	for object, candidate := range options.Watchers {
		if _, ok := object.(*v1beta1.Versions); ok {
			versionsWatch = candidate
			break
		}
	}
	require.NotNil(t, versionsWatch.Handler)
	handler, _ := versionsWatch.Handler(mgr, nil, &v1beta1.Search{})

	tests := []struct {
		name      string
		oldSpec   map[string]string
		newSpec   map[string]string
		wantQueue int
	}{
		{
			name:      "unchanged catalog is ignored",
			oldSpec:   map[string]string{"search": "v1.0.0", "ledger": "v2.9.0"},
			newSpec:   map[string]string{"search": "v1.0.0", "ledger": "v2.9.0"},
			wantQueue: 0,
		},
		{
			name:      "dependency entry change requeues consumer",
			oldSpec:   map[string]string{"search": "v1.0.0", "ledger": "v2.9.0"},
			newSpec:   map[string]string{"search": "v1.0.0", "ledger": "v3.0.0-0"},
			wantQueue: 1,
		},
		{
			name:      "unrelated entry change is ignored",
			oldSpec:   map[string]string{"search": "v1.0.0", "ledger": "v2.9.0", "gateway": "v1.0.0"},
			newSpec:   map[string]string{"search": "v1.0.0", "ledger": "v2.9.0", "gateway": "v2.0.0"},
			wantQueue: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := workqueue.NewTypedRateLimitingQueue(
				workqueue.DefaultTypedControllerRateLimiter[reconcile.Request](),
			)
			defer queue.ShutDown()
			handler.Update(context.Background(), event.TypedUpdateEvent[client.Object]{
				ObjectOld: &v1beta1.Versions{ObjectMeta: metav1.ObjectMeta{Name: "catalog"}, Spec: test.oldSpec},
				ObjectNew: &v1beta1.Versions{ObjectMeta: metav1.ObjectMeta{Name: "catalog"}, Spec: test.newSpec},
			}, queue)
			require.Equal(t, test.wantQueue, queue.Len())
		})
	}
}

func TestRequirementWatchRequeuesConsumerInDependencyStack(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	consumer := &v1beta1.Search{
		ObjectMeta: metav1.ObjectMeta{Name: "search"},
		Spec: v1beta1.SearchSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack"},
		},
	}
	consumerIndex := func(object client.Object) []string {
		unstructuredObject := object.(*unstructured.Unstructured)
		value, found, err := unstructured.NestedString(unstructuredObject.Object, "spec", "stack")
		if err == nil && found {
			return []string{value}
		}
		return nil
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(consumer).
		WithIndex(&v1beta1.Search{}, "stack", consumerIndex).
		Build()
	mgr := reconcileTestManager{client: fakeClient, scheme: scheme}

	options := ReconcilerOptions[*v1beta1.Search]{
		Watchers: map[client.Object]ReconcilerOptionsWatch{},
	}
	for _, option := range withRequirementWatches[*v1beta1.Search](
		Requirements(Require(&v1beta1.Broker{}, Ready())), nil,
	) {
		option(&options)
	}
	var brokerWatch ReconcilerOptionsWatch
	for object, candidate := range options.Watchers {
		if _, ok := object.(*v1beta1.Broker); ok {
			brokerWatch = candidate
			break
		}
	}
	require.NotNil(t, brokerWatch.Handler)
	handler, _ := brokerWatch.Handler(mgr, nil, &v1beta1.Search{})

	assertRequest := func(t *testing.T, queue workqueue.TypedRateLimitingInterface[reconcile.Request], expected int) {
		t.Helper()
		require.Equal(t, expected, queue.Len())
		if expected == 0 {
			return
		}
		request, shutdown := queue.Get()
		require.False(t, shutdown)
		require.Equal(t, consumer.Name, request.Name)
		queue.Done(request)
	}

	t.Run("create requeues consumer", func(t *testing.T) {
		queue := newRequirementsTestQueue()
		defer queue.ShutDown()
		handler.Create(context.Background(), event.TypedCreateEvent[client.Object]{
			Object: brokerForRequirementsTest("broker", false),
		}, queue)
		assertRequest(t, queue, 1)
	})

	t.Run("ready status update requeues consumer", func(t *testing.T) {
		queue := newRequirementsTestQueue()
		defer queue.ShutDown()
		handler.Update(context.Background(), event.TypedUpdateEvent[client.Object]{
			ObjectOld: brokerForRequirementsTest("broker", false),
			ObjectNew: brokerForRequirementsTest("broker", true),
		}, queue)
		assertRequest(t, queue, 1)
	})

	t.Run("dependency in another Stack is ignored", func(t *testing.T) {
		queue := newRequirementsTestQueue()
		defer queue.ShutDown()
		broker := brokerForRequirementsTest("broker", true)
		broker.Spec.Stack = "another-stack"
		handler.Create(context.Background(), event.TypedCreateEvent[client.Object]{Object: broker}, queue)
		assertRequest(t, queue, 0)
	})
}

func newRequirementsTestQueue() workqueue.TypedRateLimitingInterface[reconcile.Request] {
	return workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[reconcile.Request](),
	)
}

func newRequirementsTestContext(t *testing.T, objects ...client.Object) (testContext, *v1beta1.Stack) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack"}}
	allObjects := append([]client.Object{stack}, objects...)
	stackIndex := func(object client.Object) []string {
		if unstructuredObject, ok := object.(*unstructured.Unstructured); ok {
			value, found, err := unstructured.NestedString(unstructuredObject.Object, "spec", "stack")
			if err == nil && found {
				return []string{value}
			}
			return nil
		}
		if dependency, ok := object.(v1beta1.Dependent); ok {
			return []string{dependency.GetStack()}
		}
		return nil
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(allObjects...).
		WithIndex(&v1beta1.Broker{}, "stack", stackIndex).
		WithIndex(&v1beta1.Ledger{}, "stack", stackIndex).
		Build()

	return testContext{
		Context:   context.Background(),
		client:    fakeClient,
		apiReader: fakeClient,
		scheme:    scheme,
	}, stack
}

func brokerForRequirementsTest(name string, ready bool) *v1beta1.Broker {
	return &v1beta1.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1beta1.BrokerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack"},
		},
		Status: v1beta1.BrokerStatus{
			Status: v1beta1.Status{Ready: ready},
		},
	}
}
