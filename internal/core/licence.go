package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	golibslicence "github.com/formancehq/go-libs/v5/pkg/authn/licence"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

const (
	licenceClusterNamespace = "kube-system"
	licenceServiceName      = "operator"
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

type licenceValidator func(token string, issuer string, clusterID string) (LicenceState, string)

var validateLicenceToken = validateLicenceTokenWithGoLibs

// ValidateLicenceToken validates a licence JWT token and returns the licence state and a human-readable message.
func ValidateLicenceToken(token string, issuer string, clusterID string) (LicenceState, string) {
	return validateLicenceToken(token, issuer, clusterID)
}

func validateLicenceTokenWithGoLibs(token string, issuer string, clusterID string) (LicenceState, string) {
	if token == "" {
		return LicenceStateAbsent, ""
	}
	if issuer == "" {
		return LicenceStateInvalid, "licence issuer is required"
	}
	if clusterID == "" {
		return LicenceStateInvalid, "licence cluster ID is required"
	}

	licence := golibslicence.NewLicence(
		logging.Testing(),
		token,
		time.Hour,
		licenceServiceName,
		clusterID,
		issuer,
	)

	licenceErrors := make(chan error, 1)
	if err := licence.Start(licenceErrors); err != nil {
		return licenceStateFromError(err)
	}
	licence.Stop()

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

func resolveLicenceClusterID(reader client.Reader) (string, error) {
	namespace := &corev1.Namespace{}
	if err := reader.Get(context.Background(), types.NamespacedName{Name: licenceClusterNamespace}, namespace); err != nil {
		return "", fmt.Errorf("failed to read %q namespace: %w", licenceClusterNamespace, err)
	}
	if namespace.UID == "" {
		return "", fmt.Errorf("%q namespace has no UID", licenceClusterNamespace)
	}
	return string(namespace.UID), nil
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

	clusterID, err := resolveLicenceClusterID(reader)
	if err != nil {
		return LicenceStateInvalid, fmt.Sprintf("failed to resolve licence cluster ID: %s", err)
	}

	return ValidateLicenceToken(string(token), string(issuer), clusterID)
}
