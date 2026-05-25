package core

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testLicenceIssuer = "https://license.formance.cloud/keys"

func newLicenceTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

func newLicenceTestSecret(name string, namespace string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
}

func newSignedLicenceToken(t *testing.T, issuer string, expiresAt time.Time) string {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)

	originalPublicKey := licencePublicKey
	licencePublicKey = string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}))
	t.Cleanup(func() { licencePublicKey = originalPublicKey })

	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   "some-other-cluster",
		Audience:  []string{"some-other-service"},
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}).SignedString(privateKey)
	require.NoError(t, err)

	return token
}

func TestValidateLicenceToken_EmptyToken(t *testing.T) {
	state, msg := ValidateLicenceToken("", testLicenceIssuer)
	require.Equal(t, LicenceStateAbsent, state)
	require.Empty(t, msg)
}

func TestValidateLicenceToken_MissingIssuer(t *testing.T) {
	state, msg := ValidateLicenceToken("token", "")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "issuer")
}

func TestValidateLicenceToken_MalformedToken(t *testing.T) {
	state, msg := ValidateLicenceToken("not-a-jwt", testLicenceIssuer)
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "validation failed")
	require.Contains(t, msg, "token is malformed")
}

func TestValidateLicenceToken_DoesNotRequireAudienceOrSubject(t *testing.T) {
	token := newSignedLicenceToken(t, testLicenceIssuer, time.Now().Add(time.Hour))

	state, msg := ValidateLicenceToken(token, testLicenceIssuer)
	require.Equal(t, LicenceStateValid, state)
	require.Empty(t, msg)
}

func TestValidateLicenceToken_Expired(t *testing.T) {
	token := newSignedLicenceToken(t, testLicenceIssuer, time.Now().Add(-time.Hour))

	state, msg := ValidateLicenceToken(token, testLicenceIssuer)
	require.Equal(t, LicenceStateExpired, state)
	require.Contains(t, msg, "expired")
}

func TestLicenceStateFromError_Expired(t *testing.T) {
	state, msg := licenceStateFromError(errors.New("token has invalid claims: token is expired"))
	require.Equal(t, LicenceStateExpired, state)
	require.Contains(t, msg, "expired")
}

func TestResolveLicenceState_ValidSecret(t *testing.T) {
	reader := newLicenceTestClient(t,
		newLicenceTestSecret("licence", "operator", map[string][]byte{
			"token":  []byte("token"),
			"issuer": []byte(testLicenceIssuer),
		}),
	)

	SetLicenceValidatorForTest(t, func(token string, issuer string) (LicenceState, string) {
		require.Equal(t, "token", token)
		require.Equal(t, testLicenceIssuer, issuer)
		return LicenceStateValid, ""
	})

	state, msg := ResolveLicenceState(reader, "licence", "operator")
	require.Equal(t, LicenceStateValid, state)
	require.Empty(t, msg)
}

func TestResolveLicenceState_SecretNotFound(t *testing.T) {
	reader := newLicenceTestClient(t)

	state, msg := ResolveLicenceState(reader, "missing", "operator")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "not found")
}

func TestResolveLicenceState_MissingToken(t *testing.T) {
	reader := newLicenceTestClient(t,
		newLicenceTestSecret("licence", "operator", map[string][]byte{
			"issuer": []byte(testLicenceIssuer),
		}),
	)

	state, msg := ResolveLicenceState(reader, "licence", "operator")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "token")
}

func TestResolveLicenceState_MissingIssuer(t *testing.T) {
	reader := newLicenceTestClient(t,
		newLicenceTestSecret("licence", "operator", map[string][]byte{
			"token": []byte("token"),
		}),
	)

	state, msg := ResolveLicenceState(reader, "licence", "operator")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "issuer")
}

func TestLicenceState_String(t *testing.T) {
	require.Equal(t, "Absent", LicenceStateAbsent.String())
	require.Equal(t, "Valid", LicenceStateValid.String())
	require.Equal(t, "Expired", LicenceStateExpired.String())
	require.Equal(t, "Invalid", LicenceStateInvalid.String())
}
