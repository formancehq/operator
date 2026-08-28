package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

type testContext struct {
	context.Context
	client    client.Client
	apiReader client.Reader
	scheme    *runtime.Scheme
	platform  Platform
}

func (t testContext) GetClient() client.Client    { return t.client }
func (t testContext) GetScheme() *runtime.Scheme  { return t.scheme }
func (t testContext) GetAPIReader() client.Reader { return t.apiReader }
func (t testContext) GetPlatform() Platform       { return t.platform }

func TestForObjectControllerPropagatesApplicationErrorRequeue(t *testing.T) {
	delay := 5 * time.Second
	search := &v1beta1.Search{}
	controller := ForObjectController(func(_ Context, _ *ReconcilerOptions[*v1beta1.Search], _ *v1beta1.Search) error {
		return NewPendingError().WithRequeueAfter(delay)
	})

	err := controller(testContext{Context: context.Background()}, &ReconcilerOptions[*v1beta1.Search]{}, search)
	require.Error(t, err)
	require.Equal(t, delay, ApplicationErrorRequeueAfter(err))
	require.False(t, search.Status.Ready)
}

func TestForModulePassesRefreshedLicenceState(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack",
			UID:  types.UID("stack-uid"),
		},
		Spec: v1beta1.StackSpec{
			Version: "v2.2.0",
		},
	}
	search := &v1beta1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name: "search",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "formance.com/v1beta1",
				Kind:       "Stack",
				Name:       stack.Name,
				UID:        stack.UID,
			}},
		},
		Spec: v1beta1.SearchSpec{
			StackDependency: v1beta1.StackDependency{Stack: stack.Name},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "formance-licence",
			Namespace: "operator",
		},
		Data: map[string][]byte{
			"token":  []byte("token"),
			"issuer": []byte(testLicenceIssuer),
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(stack, search, secret).
		Build()

	SetLicenceValidatorForTest(t, func(token string, issuer string) (LicenceState, string) {
		require.Equal(t, "token", token)
		require.Equal(t, testLicenceIssuer, issuer)
		return LicenceStateValid, ""
	})

	ctx := testContext{
		Context:   context.Background(),
		client:    fakeClient,
		apiReader: fakeClient,
		scheme:    scheme,
		platform: Platform{
			LicenceSecret:    secret.Name,
			LicenceNamespace: secret.Namespace,
			LicenceState:     LicenceStateInvalid,
			LicenceMessage:   "startup state",
		},
	}

	called := false
	controller := ForModule(NoRequirements(), func(ctx Context, stack *v1beta1.Stack, reconcilerOptions *ReconcilerOptions[*v1beta1.Search], req *v1beta1.Search, version string) error {
		called = true
		require.Equal(t, LicenceStateValid, ctx.GetPlatform().LicenceState)
		require.Empty(t, ctx.GetPlatform().LicenceMessage)
		return nil
	})

	err := controller(ctx, stack, &ReconcilerOptions[*v1beta1.Search]{}, search)
	require.NoError(t, err)
	require.True(t, called)
}

func TestForModuleDisablesOwnedResourcesWithoutRunningFinalizers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack",
			UID:  types.UID("stack-uid"),
		},
		Spec: v1beta1.StackSpec{
			Disabled: true,
		},
	}
	search := &v1beta1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name: "search",
			UID:  types.UID("search-uid"),
		},
		Spec: v1beta1.SearchSpec{
			StackDependency: v1beta1.StackDependency{Stack: stack.Name},
		},
	}
	ownerController := true
	owned := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Namespace: stack.Name,
		Name:      "owned",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion:         "formance.com/v1beta1",
			Kind:               "Search",
			Name:               search.Name,
			UID:                search.UID,
			Controller:         &ownerController,
			BlockOwnerDeletion: &ownerController,
		}},
	}}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(stack, search, owned).
		Build()
	ctx := testContext{
		Context:   context.Background(),
		client:    fakeClient,
		apiReader: fakeClient,
		scheme:    scheme,
	}

	controller := ForModule(NoRequirements(), func(_ Context, _ *v1beta1.Stack, _ *ReconcilerOptions[*v1beta1.Search], _ *v1beta1.Search, _ string) error {
		t.Fatal("underlying controller must not run for a disabled Stack")
		return nil
	})
	options := &ReconcilerOptions[*v1beta1.Search]{
		Owns: map[client.Object][]builder.OwnsOption{&corev1.ConfigMap{}: nil},
	}
	finalizerCalled := false
	WithFinalizer("delete", func(_ Context, _ *v1beta1.Search) error {
		finalizerCalled = true
		return nil
	})(options)

	require.NoError(t, controller(ctx, stack, options, search))
	require.False(t, finalizerCalled)
	refreshedSearch := &v1beta1.Search{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKey{Name: search.Name}, refreshedSearch))
	require.Len(t, refreshedSearch.OwnerReferences, 1)
	require.Equal(t, stack.Name, refreshedSearch.OwnerReferences[0].Name)
	require.Equal(t, stack.UID, refreshedSearch.OwnerReferences[0].UID)
	err := fakeClient.Get(ctx, types.NamespacedName{Namespace: owned.Namespace, Name: owned.Name}, &corev1.ConfigMap{})
	require.True(t, apierrors.IsNotFound(err), "owned runtime resource must be deleted when the Stack is disabled")
}
