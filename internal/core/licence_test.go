package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

func newClusterNamespace(uid string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: licenceClusterNamespace,
			UID:  types.UID(uid),
		},
	}
}

func TestValidateLicenceToken_EmptyToken(t *testing.T) {
	state, msg := ValidateLicenceToken("", testLicenceIssuer, "cluster-id")
	require.Equal(t, LicenceStateAbsent, state)
	require.Empty(t, msg)
}

func TestValidateLicenceToken_MissingIssuer(t *testing.T) {
	state, msg := ValidateLicenceToken("token", "", "cluster-id")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "issuer")
}

func TestValidateLicenceToken_MissingClusterID(t *testing.T) {
	state, msg := ValidateLicenceToken("token", testLicenceIssuer, "")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "cluster ID")
}

func TestValidateLicenceToken_UsesGoLibsValidator(t *testing.T) {
	state, msg := ValidateLicenceToken("not-a-jwt", testLicenceIssuer, "cluster-id")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "validation failed")
	require.Contains(t, msg, "token is malformed")
}

func TestLicenceStateFromError_Expired(t *testing.T) {
	state, msg := licenceStateFromError(errors.New("token has invalid claims: token is expired"))
	require.Equal(t, LicenceStateExpired, state)
	require.Contains(t, msg, "expired")
}

func TestResolveLicenceState_ValidSecret(t *testing.T) {
	reader := newLicenceTestClient(t,
		newClusterNamespace("cluster-id"),
		newLicenceTestSecret("licence", "operator", map[string][]byte{
			"token":  []byte("token"),
			"issuer": []byte(testLicenceIssuer),
		}),
	)

	SetLicenceValidatorForTest(t, func(token string, issuer string, clusterID string) (LicenceState, string) {
		require.Equal(t, "token", token)
		require.Equal(t, testLicenceIssuer, issuer)
		require.Equal(t, "cluster-id", clusterID)
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

func TestResolveLicenceState_MissingClusterNamespace(t *testing.T) {
	reader := newLicenceTestClient(t,
		newLicenceTestSecret("licence", "operator", map[string][]byte{
			"token":  []byte("token"),
			"issuer": []byte(testLicenceIssuer),
		}),
	)

	state, msg := ResolveLicenceState(reader, "licence", "operator")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "cluster ID")
	require.Contains(t, msg, licenceClusterNamespace)
}

func TestLicenceState_String(t *testing.T) {
	require.Equal(t, "Absent", LicenceStateAbsent.String())
	require.Equal(t, "Valid", LicenceStateValid.String())
	require.Equal(t, "Expired", LicenceStateExpired.String())
	require.Equal(t, "Invalid", LicenceStateInvalid.String())
}
