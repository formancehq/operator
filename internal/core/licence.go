package core

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// This is the Formance licence public key from https://license.formance.cloud/keys.
	// It intentionally lives here instead of using go-libs' Licence helper because
	// the operator only needs to verify token authenticity, issuer, and expiration.
	// It must not require operator-specific aud/sub claims.
	licencePublicKey = `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA46LVe+BCO/go0MoKM4r7
exTGeFSz10ra/hpFK0XJGVm6W42GTjFzNlNTCKQZBkF63STYK+o+FEFmSgMVxTjf
qA4GZGxYddukT4pNR+WaRLQSPxPkMsGrzoORtq8n2v4Y+m5jvYDXhLLmYsDNxVuv
SrAOtgJ0Ac8jJWXEu8Eqs0ferl9ftLRqrN+RfpXATT4fAgHBxVl5u1mFsQX6lo1B
N5m099Ni50Cmlauun883bS8xzLt/XLlk6vBaJKhfyDbkjcA4qN+33f5mih4v6EBP
txyeCg9yhHOfga61owAI+FOGEVW1OMTQ3PP/d2buiw9YrRAtBEXsJdhovc84jwmJ
sjA829+2nFR1Bq3jQ8nG4iTnF9yIwJr+l9reoV8Butskwld9mhry+dIimGpVUmy3
psYmj910D1eH+tyuCGN7YAjD5+bXVUBPGfD1kJExtzjjyYruXD6trt7nchWrJIOu
D1I0OT3j+PWASm0c/AdN8BcV96HZhJBbCDK5GaQ9HSw+GVEpaqP9TY4uEz2werNq
cvjYlBS4FocA0ClsaDs9llIZVrI7kPYIeoO2KNWn7kp1q+awrNt677MLFmj7eqZ/
jl/Sx2brq8e91kTG57Z2qRTkSGkCK20NFOI8E+m9bhhVRFw4RhY6g3lH1B5hd+dd
6TCk5eN7hTkosG21POe9goUCAwEAAQ==
-----END PUBLIC KEY-----`
)

// LicenceState represents the current state of the licence in the operator.
type LicenceState int

const (
	LicenceStateAbsent  LicenceState = iota // No licence configured
	LicenceStateValid                       // JWT present and valid
	LicenceStateExpired                     // JWT present but expired
	LicenceStateInvalid                     // JWT present but malformed/bad signature
)

func (s LicenceState) String() string {
	switch s {
	case LicenceStateAbsent:
		return "Absent"
	case LicenceStateValid:
		return "Valid"
	case LicenceStateExpired:
		return "Expired"
	case LicenceStateInvalid:
		return "Invalid"
	default:
		return "Unknown"
	}
}

type licenceValidator func(token string, issuer string) (LicenceState, string)

var validateLicenceToken = validateLicenceTokenWithPublicKey

// ValidateLicenceToken validates a licence JWT token and returns the licence state and a human-readable message.
func ValidateLicenceToken(token string, issuer string) (LicenceState, string) {
	return validateLicenceToken(token, issuer)
}

func validateLicenceTokenWithPublicKey(token string, issuer string) (LicenceState, string) {
	if token == "" {
		return LicenceStateAbsent, ""
	}
	if issuer == "" {
		return LicenceStateInvalid, "licence issuer is required"
	}

	key, err := getLicencePublicKey()
	if err != nil {
		return LicenceStateInvalid, err.Error()
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(issuer),
	)

	parsedToken, err := parser.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return key, nil
	})
	if err != nil {
		return licenceStateFromError(err)
	}
	if !parsedToken.Valid {
		return LicenceStateInvalid, "licence token is not valid"
	}

	return LicenceStateValid, ""
}

func getLicencePublicKey() (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(licencePublicKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode embedded Formance licence public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse embedded Formance licence public key: %w", err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("embedded Formance licence public key is not RSA")
	}

	return rsaPub, nil
}

func licenceStateFromError(err error) (LicenceState, string) {
	if err == nil {
		return LicenceStateValid, ""
	}

	message := fmt.Sprintf("licence token validation failed: %s", err)
	if strings.Contains(strings.ToLower(err.Error()), "expired") {
		return LicenceStateExpired, "licence token is expired"
	}

	return LicenceStateInvalid, message
}

// SetLicenceValidatorForTest overrides licence validation for tests and restores it on cleanup.
func SetLicenceValidatorForTest(t interface {
	Helper()
	Cleanup(func())
}, validator licenceValidator) {
	t.Helper()
	original := validateLicenceToken
	validateLicenceToken = validator
	t.Cleanup(func() { validateLicenceToken = original })
}

// ResolveLicenceState reads the licence Secret by name from the configured licence namespace,
// extracts the JWT token, and validates it. This is called during each EE reconciliation
// to ensure the licence state is always fresh (not stale from startup).
func ResolveLicenceState(reader client.Reader, secretName string, licenceNamespace string) (LicenceState, string) {
	if secretName == "" {
		return LicenceStateAbsent, ""
	}

	secret := &corev1.Secret{}
	err := reader.Get(context.Background(), types.NamespacedName{
		Name:      secretName,
		Namespace: licenceNamespace,
	}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return LicenceStateInvalid, fmt.Sprintf("licence secret %q not found in namespace %q", secretName, licenceNamespace)
		}
		return LicenceStateInvalid, fmt.Sprintf("failed to read licence secret %q: %s", secretName, err)
	}

	token, ok := secret.Data["token"]
	if !ok || len(token) == 0 {
		return LicenceStateInvalid, "licence secret missing non-empty 'token' key"
	}

	issuer, ok := secret.Data["issuer"]
	if !ok || len(issuer) == 0 {
		return LicenceStateInvalid, "licence secret missing non-empty 'issuer' key"
	}

	return ValidateLicenceToken(string(token), string(issuer))
}
