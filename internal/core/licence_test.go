package core

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func generateTestRSAKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)

	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	return privateKey, string(pubPEM)
}

func setTestKey(t *testing.T, key string) {
	t.Helper()
	SetFormancePublicKeyForTest(t, key)
}

func createToken(t *testing.T, claims jwt.MapClaims, key *rsa.PrivateKey) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := token.SignedString(key)
	require.NoError(t, err)
	return s
}

func TestValidateLicenceToken_EmptyToken(t *testing.T) {
	state, msg := ValidateLicenceToken("")
	require.Equal(t, LicenceStateAbsent, state)
	require.Empty(t, msg)
}

func TestValidateLicenceToken_ValidToken(t *testing.T) {
	privateKey, pubPEM := generateTestRSAKeyPair(t)
	setTestKey(t, pubPEM)

	token := createToken(t, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	}, privateKey)

	state, msg := ValidateLicenceToken(token)
	require.Equal(t, LicenceStateValid, state)
	require.Empty(t, msg)
}

func TestValidateLicenceToken_ExpiredToken(t *testing.T) {
	privateKey, pubPEM := generateTestRSAKeyPair(t)
	setTestKey(t, pubPEM)

	token := createToken(t, jwt.MapClaims{
		"exp": time.Now().Add(-time.Hour).Unix(),
	}, privateKey)

	state, msg := ValidateLicenceToken(token)
	require.Equal(t, LicenceStateExpired, state)
	require.Contains(t, msg, "expired")
}

func TestValidateLicenceToken_MalformedToken(t *testing.T) {
	state, msg := ValidateLicenceToken("not-a-jwt")
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "validation failed")
}

func TestValidateLicenceToken_WrongSigningKey(t *testing.T) {
	_, pubPEM := generateTestRSAKeyPair(t)
	setTestKey(t, pubPEM)

	otherKey, _ := generateTestRSAKeyPair(t)
	token := createToken(t, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	}, otherKey)

	state, msg := ValidateLicenceToken(token)
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "validation failed")
}

func TestValidateLicenceToken_NoExpiration(t *testing.T) {
	privateKey, pubPEM := generateTestRSAKeyPair(t)
	setTestKey(t, pubPEM)

	token := createToken(t, jwt.MapClaims{}, privateKey)

	state, msg := ValidateLicenceToken(token)
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "validation failed")
}

func TestValidateLicenceToken_ProductionKeyParses(t *testing.T) {
	// Verify the embedded production key can be parsed without error.
	// Token is signed with a random key, so validation must fail with
	// a signature error, not a key parsing error.
	otherKey, _ := generateTestRSAKeyPair(t)
	token := createToken(t, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	}, otherKey)

	state, msg := ValidateLicenceToken(token)
	require.Equal(t, LicenceStateInvalid, state)
	require.Contains(t, msg, "validation failed")
	require.NotContains(t, msg, "public key")
}

func TestLicenceState_String(t *testing.T) {
	require.Equal(t, "Absent", LicenceStateAbsent.String())
	require.Equal(t, "Valid", LicenceStateValid.String())
	require.Equal(t, "Expired", LicenceStateExpired.String())
	require.Equal(t, "Invalid", LicenceStateInvalid.String())
}
