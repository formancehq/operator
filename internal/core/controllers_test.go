package core

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

func TestForModulePassesRefreshedLicenceState(t *testing.T) {
	privateKey, pubPEM := generateTestRSAKeyPair(t)
	setTestKey(t, pubPEM)

	token := createToken(t, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
		"iss": testLicenceIssuer,
	}, privateKey)

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack",
			UID:  types.UID("stack-uid"),
		},
		Spec: v1beta1.StackSpec{
			Version: "v1.0.0",
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
			"token":  []byte(token),
			"issuer": []byte(testLicenceIssuer),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(stack, search, secret).
		Build()

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
	controller := ForModule(func(ctx Context, stack *v1beta1.Stack, reconcilerOptions *ReconcilerOptions[*v1beta1.Search], req *v1beta1.Search, version string) error {
		called = true
		require.Equal(t, LicenceStateValid, ctx.GetPlatform().LicenceState)
		require.Empty(t, ctx.GetPlatform().LicenceMessage)
		return nil
	})

	err := controller(ctx, stack, &ReconcilerOptions[*v1beta1.Search]{}, search)
	require.NoError(t, err)
	require.True(t, called)
}
