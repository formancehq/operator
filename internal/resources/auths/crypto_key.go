package auths

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
)

const (
	cryptoKeySecretName = "auth-crypto-key"
	cryptoKeySecretData = "crypto-key"
	// cryptoKeyLength is the exact number of bytes the CRYPTO_KEY must
	// decode to (a 256 bits symmetric key).
	cryptoKeyLength = 32
)

// ensureCryptoKeySecret returns the per-stack crypto key secret.
//
// The key is generated once, with crypto/rand, only when the Secret does not
// exist at all. An existing Secret is never mutated: a missing, empty or
// malformed "crypto-key" entry is an explicit reconciliation error, so a bad
// value surfaces instead of being silently replaced (a silent rotation would
// make every previously encrypted artifact unreadable).
func ensureCryptoKeySecret(ctx Context, auth *v1beta1.Auth) (*corev1.Secret, error) {
	key := types.NamespacedName{
		Namespace: auth.Spec.Stack,
		Name:      cryptoKeySecretName,
	}

	existing := &corev1.Secret{}
	err := ctx.GetClient().Get(ctx, key, existing)
	switch {
	case apierrors.IsNotFound(err):
		return createCryptoKeySecret(ctx, auth, key)
	case err != nil:
		return nil, fmt.Errorf("retrieving secret %s/%s: %w", key.Namespace, key.Name, err)
	}

	if err := validateExistingCryptoKeySecret(existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// createCryptoKeySecret generates the crypto key exactly once, when the Secret
// is genuinely absent. If a concurrent actor wins the creation race, the
// existing Secret is read back and validated, never overwritten.
func createCryptoKeySecret(ctx Context, auth *v1beta1.Auth, key types.NamespacedName) (*corev1.Secret, error) {
	raw := make([]byte, cryptoKeyLength)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generating crypto key: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: key.Namespace,
			Name:      key.Name,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			// The entry holds the base64 STRING bytes; the Kubernetes Secret
			// Data map itself adds the separate Kubernetes encoding layer.
			cryptoKeySecretData: []byte(base64.StdEncoding.EncodeToString(raw)),
		},
	}
	if err := controllerutil.SetControllerReference(auth, secret, ctx.GetScheme()); err != nil {
		return nil, err
	}
	if err := ctx.GetClient().Create(ctx, secret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		created := &corev1.Secret{}
		if err := ctx.GetClient().Get(ctx, key, created); err != nil {
			return nil, fmt.Errorf("retrieving concurrently created secret %s/%s: %w", key.Namespace, key.Name, err)
		}
		if err := validateExistingCryptoKeySecret(created); err != nil {
			return nil, err
		}
		return created, nil
	}
	return secret, nil
}

// validateExistingCryptoKeySecret rejects a pre-existing Secret with a
// missing or empty crypto-key entry instead of rotating the key silently.
func validateExistingCryptoKeySecret(secret *corev1.Secret) error {
	if secret.Data == nil || len(secret.Data[cryptoKeySecretData]) == 0 {
		return fmt.Errorf("secret %s/%s already exists but its %q entry is missing or empty: populate it with a valid crypto key or delete the Secret explicitly, the operator never overwrites an existing crypto key secret",
			secret.Namespace, secret.Name, cryptoKeySecretData)
	}
	if err := validateCryptoKey(secret.Data[cryptoKeySecretData]); err != nil {
		return fmt.Errorf("invalid crypto key in secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}
	return nil
}

// validateCryptoKey checks that the stored value is the canonical, strict,
// standard padded base64 encoding of exactly cryptoKeyLength bytes.
//
// base64.StdEncoding.DecodeString alone is too lenient for the fixed contract:
// it tolerates CR/LF inside the payload and non-zero padding bits, and a
// URL-safe alphabet or unpadded form can still decode. A re-encoding
// round-trip rejects those non-canonical forms explicitly.
func validateCryptoKey(value []byte) error {
	if len(value) == 0 {
		return fmt.Errorf("crypto key is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(string(value))
	if err != nil {
		return fmt.Errorf("crypto key must be a standard padded base64 string: %w", err)
	}
	if len(decoded) != cryptoKeyLength {
		return fmt.Errorf("crypto key must decode to exactly %d bytes, got %d", cryptoKeyLength, len(decoded))
	}
	if !bytes.Equal(value, []byte(base64.StdEncoding.EncodeToString(decoded))) {
		return fmt.Errorf("crypto key must be the canonical standard padded base64 encoding: CR/LF, whitespace, URL-safe alphabet or non-canonical padding are rejected")
	}
	return nil
}
