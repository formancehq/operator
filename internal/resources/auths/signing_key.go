package auths

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
)

const (
	signingKeySecretName = "auth-signing-key"
	signingKeySecretData = "signing-key"
	signingKeyBits       = 2048
)

func ensureSigningKeySecret(ctx Context, auth *v1beta1.Auth) (*corev1.Secret, error) {
	secret, _, err := CreateOrUpdate[*corev1.Secret](ctx, types.NamespacedName{
		Namespace: auth.Spec.Stack,
		Name:      signingKeySecretName,
	}, func(secret *corev1.Secret) error {
		secret.Type = corev1.SecretTypeOpaque
		if len(secret.Data[signingKeySecretData]) == 0 {
			privateKey, err := rsa.GenerateKey(rand.Reader, signingKeyBits)
			if err != nil {
				return err
			}
			encodedKey := pem.EncodeToMemory(&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
			})
			if secret.Data == nil {
				secret.Data = make(map[string][]byte)
			}
			secret.Data[signingKeySecretData] = encodedKey
		}
		return nil
	}, WithController[*corev1.Secret](ctx.GetScheme(), auth))
	if err != nil {
		return nil, err
	}

	return secret, nil
}
