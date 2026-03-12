package stacks

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type mockContext struct {
	context.Context
	platform  core.Platform
	apiReader client.Reader
}

func (m *mockContext) GetClient() client.Client    { return nil }
func (m *mockContext) GetScheme() *runtime.Scheme  { return nil }
func (m *mockContext) GetAPIReader() client.Reader { return m.apiReader }
func (m *mockContext) GetPlatform() core.Platform  { return m.platform }

func newMockContext(platform core.Platform, objects ...client.Object) core.Context {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}
	return &mockContext{
		Context:   context.Background(),
		platform:  platform,
		apiReader: builder.Build(),
	}
}

func newLicenceSecret(name, namespace, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"token":  []byte(token),
			"issuer": []byte("https://license.formance.cloud/keys"),
		},
	}
}

func generateValidToken(t *testing.T) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	// Override the embedded key for testing
	core.SetFormancePublicKeyForTest(t, string(pubPEM))

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	s, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return s
}

func generateExpiredToken(t *testing.T) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	core.SetFormancePublicKeyForTest(t, string(pubPEM))

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	s, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return s
}

func newStack(name string) *v1beta1.Stack {
	return &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Generation: 1,
		},
	}
}

func TestSetLicenceCondition_NoLicence(t *testing.T) {
	stack := newStack("test")
	ctx := newMockContext(core.Platform{LicenceSecret: ""})
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.Nil(t, cond, "no condition should be set when licence is absent")
}

func TestSetLicenceCondition_ValidLicence(t *testing.T) {
	stack := newStack("test")
	token := generateValidToken(t)
	secret := newLicenceSecret("my-secret", "operator", token)
	ctx := newMockContext(core.Platform{
		LicenceSecret:    "my-secret",
		LicenceNamespace: "operator",
	}, secret)
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, "Valid", cond.Reason)
}

func TestSetLicenceCondition_ExpiredLicence(t *testing.T) {
	stack := newStack("test")
	token := generateExpiredToken(t)
	secret := newLicenceSecret("my-secret", "operator", token)
	ctx := newMockContext(core.Platform{
		LicenceSecret:    "my-secret",
		LicenceNamespace: "operator",
	}, secret)
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "Expired", cond.Reason)
	require.Contains(t, cond.Message, "expired")
}

func TestSetLicenceCondition_InvalidLicence(t *testing.T) {
	stack := newStack("test")
	secret := newLicenceSecret("my-secret", "operator", "not-a-jwt")
	ctx := newMockContext(core.Platform{
		LicenceSecret:    "my-secret",
		LicenceNamespace: "operator",
	}, secret)
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "Invalid", cond.Reason)
}

func TestSetLicenceCondition_EmptyToken(t *testing.T) {
	stack := newStack("test")
	secret := newLicenceSecret("my-secret", "operator", "")
	ctx := newMockContext(core.Platform{
		LicenceSecret:    "my-secret",
		LicenceNamespace: "operator",
	}, secret)
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "Invalid", cond.Reason)
}

func TestSetLicenceCondition_SecretNotFound(t *testing.T) {
	stack := newStack("test")
	// No secret created — should be Invalid
	ctx := newMockContext(core.Platform{
		LicenceSecret:    "missing-secret",
		LicenceNamespace: "operator",
	})
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "Invalid", cond.Reason)
}

func TestSetLicenceCondition_TransitionFromValidToExpired(t *testing.T) {
	stack := newStack("test")

	// First: valid
	validToken := generateValidToken(t)
	secret := newLicenceSecret("my-secret", "operator", validToken)
	ctx := newMockContext(core.Platform{
		LicenceSecret:    "my-secret",
		LicenceNamespace: "operator",
	}, secret)
	setLicenceCondition(ctx, stack)
	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)

	// Then: expired (new context with expired token)
	expiredToken := generateExpiredToken(t)
	secret2 := newLicenceSecret("my-secret", "operator", expiredToken)
	ctx2 := newMockContext(core.Platform{
		LicenceSecret:    "my-secret",
		LicenceNamespace: "operator",
	}, secret2)
	setLicenceCondition(ctx2, stack)
	cond = stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "Expired", cond.Reason)
}

func TestSetLicenceCondition_RemovedWhenSecretCleared(t *testing.T) {
	stack := newStack("test")

	// First: set with valid licence
	validToken := generateValidToken(t)
	secret := newLicenceSecret("my-secret", "operator", validToken)
	ctx := newMockContext(core.Platform{
		LicenceSecret:    "my-secret",
		LicenceNamespace: "operator",
	}, secret)
	setLicenceCondition(ctx, stack)
	require.NotNil(t, stack.Status.Conditions.Get("LicenceValid"))

	// Then: licence secret removed from platform config
	ctx2 := newMockContext(core.Platform{LicenceSecret: ""})
	setLicenceCondition(ctx2, stack)
	require.Nil(t, stack.Status.Conditions.Get("LicenceValid"))
}
