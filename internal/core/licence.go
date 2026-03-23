package core

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// formancePublicKey is the PEM-encoded RSA public key used to verify licence JWTs (RS256).
// This is the same key embedded in go-libs/v4/licence/public_key.go.
// It can be overridden at build time via ldflags:
//
//	go build -ldflags "-X github.com/formancehq/operator/v3/internal/core.formancePublicKey=$(cat key.pem)"
//
//nolint:lll
var formancePublicKey = `-----BEGIN PUBLIC KEY-----
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

func parseFormancePublicKey() (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(formancePublicKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode embedded Formance public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse embedded Formance public key: %w", err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("embedded Formance public key is not RSA")
	}

	return rsaPub, nil
}

// ValidateLicenceToken validates a licence JWT token and returns the licence state and a human-readable message.
// The validation uses the same logic as go-libs/v4/licence/jwt.go:
// RS256 signature with embedded Formance public key, expiration required.
func ValidateLicenceToken(token string) (LicenceState, string) {
	if token == "" {
		return LicenceStateAbsent, ""
	}

	rsaPub, err := parseFormancePublicKey()
	if err != nil {
		return LicenceStateInvalid, fmt.Sprintf("public key error: %s", err)
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
	)

	parsed, err := parser.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return rsaPub, nil
	})
	if err != nil {
		if isTokenExpired(err) {
			return LicenceStateExpired, "licence token is expired"
		}
		return LicenceStateInvalid, fmt.Sprintf("licence token validation failed: %s", err)
	}

	if !parsed.Valid {
		return LicenceStateInvalid, "licence token is not valid"
	}

	return LicenceStateValid, ""
}

// SetFormancePublicKeyForTest overrides the embedded public key for testing and restores it on cleanup.
func SetFormancePublicKeyForTest(t interface {
	Helper()
	Cleanup(func())
}, key string) {
	t.Helper()
	original := formancePublicKey
	formancePublicKey = key
	t.Cleanup(func() { formancePublicKey = original })
}

func isTokenExpired(err error) bool {
	return errors.Is(err, jwt.ErrTokenExpired)
}

// ResolveLicenceState reads the licence Secret by name from the operator's namespace,
// extracts the JWT token, and validates it. This is called during each EE reconciliation
// to ensure the licence state is always fresh (not stale from startup).
func ResolveLicenceState(reader client.Reader, secretName string, operatorNamespace string) (LicenceState, string) {
	if secretName == "" {
		return LicenceStateAbsent, ""
	}

	secret := &corev1.Secret{}
	err := reader.Get(context.Background(), types.NamespacedName{
		Name:      secretName,
		Namespace: operatorNamespace,
	}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return LicenceStateInvalid, fmt.Sprintf("licence secret %q not found in namespace %q", secretName, operatorNamespace)
		}
		return LicenceStateInvalid, fmt.Sprintf("failed to read licence secret %q: %s", secretName, err)
	}

	token, ok := secret.Data["token"]
	if !ok || len(token) == 0 {
		return LicenceStateInvalid, "licence secret missing non-empty 'token' key"
	}

	return ValidateLicenceToken(string(token))
}
