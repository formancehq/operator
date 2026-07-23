package databases

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

const (
	postgresCredentialsUsernameKey = "username"
	postgresCredentialsPasswordKey = "password"

	encodedPostgresCredentialsSecretSuffix              = "postgres-uri-credentials"
	collisionSafeEncodedPostgresCredentialsSecretSuffix = "encoded-postgres-uri-credentials"
	encodedPostgresCredentialsSecretAnnotation          = "formance.com/encoded-postgres-credentials-secret"
	postgresCredentialsEncodingQueryParam               = "secretCredentialsEncoding"

	postgresCredentialsEncodingURLEncoded postgresCredentialsEncoding = "urlEncoded"
	postgresCredentialsEncodingRaw        postgresCredentialsEncoding = "raw"
)

type postgresCredentialsEncoding string

func getEncodedPostgresCredentialsSecretName(database *v1beta1.Database, sourceSecretName ...string) string {
	name := fmt.Sprintf("%s-%s", database.Name, encodedPostgresCredentialsSecretSuffix)
	if len(sourceSecretName) > 0 && sourceSecretName[0] == name {
		return fmt.Sprintf("%s-%s-%s", database.Name, collisionSafeEncodedPostgresCredentialsSecretSuffix, hashPostgresCredentialsSecretName(sourceSecretName[0]))
	}
	return name
}

func hashPostgresCredentialsSecretName(secretName string) string {
	hash := sha256.Sum256([]byte(secretName))
	return hex.EncodeToString(hash[:])[:8]
}

func getEncodedPostgresCredentialsSecretCandidateNames(database *v1beta1.Database, sourceSecretName ...string) []string {
	defaultName := fmt.Sprintf("%s-%s", database.Name, encodedPostgresCredentialsSecretSuffix)
	names := []string{
		defaultName,
		fmt.Sprintf("%s-%s", database.Name, collisionSafeEncodedPostgresCredentialsSecretSuffix),
		getEncodedPostgresCredentialsSecretName(database, defaultName),
	}
	if len(sourceSecretName) > 0 && sourceSecretName[0] != "" {
		names = append(names, getEncodedPostgresCredentialsSecretName(database, sourceSecretName[0]))
	}
	return dedupePostgresCredentialsSecretNames(names)
}

func dedupePostgresCredentialsSecretNames(names []string) []string {
	ret := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		ret = append(ret, name)
	}
	return ret
}

func parsePostgresCredentialsEncoding(value string) (postgresCredentialsEncoding, error) {
	switch postgresCredentialsEncoding(value) {
	case "", postgresCredentialsEncodingURLEncoded:
		return postgresCredentialsEncodingURLEncoded, nil
	case postgresCredentialsEncodingRaw:
		return postgresCredentialsEncodingRaw, nil
	default:
		return "", fmt.Errorf("invalid %s %q: expected %q or %q",
			postgresCredentialsEncodingQueryParam,
			value,
			postgresCredentialsEncodingURLEncoded,
			postgresCredentialsEncodingRaw,
		)
	}
}

func reconcileEncodedPostgresCredentialsSecret(
	ctx core.Context,
	stack *v1beta1.Stack,
	database *v1beta1.Database,
	secretName string,
	credentialsEncoding postgresCredentialsEncoding,
) error {
	sourceSecret := &corev1.Secret{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      secretName,
	}, sourceSecret); err != nil {
		return errors.Wrap(err, "getting postgres credentials secret")
	}
	if err := removeSourceSecretControllerReference(ctx, database, sourceSecret); err != nil {
		return err
	}

	username, ok := sourceSecret.Data[postgresCredentialsUsernameKey]
	if !ok {
		return fmt.Errorf("postgres credentials secret %s/%s is missing %q", stack.Name, secretName, postgresCredentialsUsernameKey)
	}
	password, ok := sourceSecret.Data[postgresCredentialsPasswordKey]
	if !ok {
		return fmt.Errorf("postgres credentials secret %s/%s is missing %q", stack.Name, secretName, postgresCredentialsPasswordKey)
	}
	if credentialsEncoding == postgresCredentialsEncodingRaw {
		username = []byte(escapePostgresCredentialForURI(string(username)))
		password = []byte(escapePostgresCredentialForURI(string(password)))
	}

	encodedSecretName := getEncodedPostgresCredentialsSecretName(database, secretName)
	if err := ensureEncodedPostgresCredentialsSecretCanBeWritten(ctx, stack, database, encodedSecretName); err != nil {
		return err
	}
	_, _, err := core.CreateOrUpdate[*corev1.Secret](ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      encodedSecretName,
	}, func(secret *corev1.Secret) error {
		secret.Type = corev1.SecretTypeOpaque
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[encodedPostgresCredentialsSecretAnnotation] = "true"
		secret.Data = map[string][]byte{
			postgresCredentialsUsernameKey: username,
			postgresCredentialsPasswordKey: password,
		}
		return nil
	}, core.WithController[*corev1.Secret](ctx.GetScheme(), database))
	if err != nil {
		return errors.Wrap(err, "reconciling encoded postgres credentials secret")
	}

	return deleteStaleEncodedPostgresCredentialsSecrets(ctx, stack, database, secretName, encodedSecretName)
}

func ensureEncodedPostgresCredentialsSecretCanBeWritten(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, encodedSecretName string) error {
	secret := &corev1.Secret{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      encodedSecretName,
	}, secret); err != nil {
		return client.IgnoreNotFound(err)
	}
	if metav1.IsControlledBy(secret, database) {
		return nil
	}
	return fmt.Errorf("encoded postgres credentials secret %s/%s already exists and is not controlled by database %s", stack.Name, encodedSecretName, database.Name)
}

func removeSourceSecretControllerReference(ctx core.Context, database *v1beta1.Database, sourceSecret *corev1.Secret) error {
	if !metav1.IsControlledBy(sourceSecret, database) {
		return nil
	}
	patch := client.MergeFrom(sourceSecret.DeepCopy())
	if err := controllerutil.RemoveControllerReference(database, sourceSecret, ctx.GetScheme()); err != nil {
		return errors.Wrap(err, "removing stale database controller reference from postgres credentials secret")
	}
	if err := ctx.GetClient().Patch(ctx, sourceSecret, patch); err != nil {
		return errors.Wrap(err, "patching postgres credentials secret owner references")
	}
	return nil
}

func deleteEncodedPostgresCredentialsSecret(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, sourceSecretName string) error {
	for _, name := range getEncodedPostgresCredentialsSecretCandidateNames(database, sourceSecretName) {
		if name == sourceSecretName {
			if err := removeSourceSecretControllerReferenceByName(ctx, stack, database, name); err != nil {
				return errors.Wrap(err, "removing stale postgres credentials secret controller reference")
			}
			continue
		}
		if sourceSecretName == "" && name == getEncodedPostgresCredentialsSecretName(database) {
			if err := deleteOrRemediateDefaultPostgresCredentialsSecretWithoutSource(ctx, stack, database, name); err != nil {
				return err
			}
			continue
		}
		if err := deleteEncodedPostgresCredentialsSecretIfControlled(ctx, stack, database, name); err != nil {
			return errors.Wrap(err, "deleting encoded postgres credentials secret")
		}
	}
	return nil
}

func removeSourceSecretControllerReferenceByName(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, name string) error {
	secret := &corev1.Secret{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      name,
	}, secret); err != nil {
		return client.IgnoreNotFound(err)
	}
	return removeSourceSecretControllerReference(ctx, database, secret)
}

func deleteOrRemediateDefaultPostgresCredentialsSecretWithoutSource(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, name string) error {
	return handleDefaultPostgresCredentialsSecretWithoutSource(ctx, stack, database, name, true)
}

func deleteDefaultEncodedPostgresCredentialsSecretWithoutSource(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, name string) error {
	return handleDefaultPostgresCredentialsSecretWithoutSource(ctx, stack, database, name, false)
}

func handleDefaultPostgresCredentialsSecretWithoutSource(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, name string, remediateUnannotated bool) error {
	secret := &corev1.Secret{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      name,
	}, secret); err != nil {
		return client.IgnoreNotFound(err)
	}
	if secret.Annotations[encodedPostgresCredentialsSecretAnnotation] == "true" {
		if !metav1.IsControlledBy(secret, database) {
			return nil
		}
		return ctx.GetClient().Delete(ctx, secret)
	}
	if !remediateUnannotated {
		return nil
	}
	return removeSourceSecretControllerReference(ctx, database, secret)
}

func deleteStaleEncodedPostgresCredentialsSecrets(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, sourceSecretName, desiredSecretName string) error {
	for _, name := range getEncodedPostgresCredentialsSecretCandidateNames(database, sourceSecretName) {
		if name == sourceSecretName || name == desiredSecretName {
			continue
		}
		if err := deleteEncodedPostgresCredentialsSecretIfControlled(ctx, stack, database, name); err != nil {
			return errors.Wrap(err, "deleting stale encoded postgres credentials secret")
		}
	}
	return nil
}

func deleteEncodedPostgresCredentialsSecretIfControlled(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, name string) error {
	secret := &corev1.Secret{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      name,
	}, secret); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(secret, database) {
		return nil
	}
	return ctx.GetClient().Delete(ctx, secret)
}
