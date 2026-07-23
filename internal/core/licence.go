package core

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	licence "github.com/formancehq/go-libs/v5/pkg/authn/licence"
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

var validateLicenceToken = validateLicenceTokenWithGoLibs

// ValidateLicenceToken validates a licence JWT token and returns the licence state and a human-readable message.
func ValidateLicenceToken(token string, issuer string) (LicenceState, string) {
	return validateLicenceToken(token, issuer)
}

func validateLicenceTokenWithGoLibs(token string, issuer string) (LicenceState, string) {
	if token == "" {
		return LicenceStateAbsent, ""
	}
	if issuer == "" {
		return LicenceStateInvalid, "licence issuer is required"
	}

	if err := licence.ValidateToken(token, issuer); err != nil {
		return licenceStateFromError(err)
	}

	return LicenceStateValid, ""
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
