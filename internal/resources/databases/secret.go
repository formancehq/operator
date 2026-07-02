package databases

import (
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

const (
	postgresCredentialsUsernameKey = "username"
	postgresCredentialsPasswordKey = "password"

	encodedPostgresCredentialsSecretSuffix = "postgres-uri-credentials"
)

func getEncodedPostgresCredentialsSecretName(database *v1beta1.Database) string {
	return fmt.Sprintf("%s-%s", database.Name, encodedPostgresCredentialsSecretSuffix)
}

func reconcileEncodedPostgresCredentialsSecret(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, secretName string) error {
	sourceSecret := &corev1.Secret{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      secretName,
	}, sourceSecret); err != nil {
		return errors.Wrap(err, "getting postgres credentials secret")
	}

	username, ok := sourceSecret.Data[postgresCredentialsUsernameKey]
	if !ok {
		return fmt.Errorf("postgres credentials secret %s/%s is missing %q", stack.Name, secretName, postgresCredentialsUsernameKey)
	}
	password, ok := sourceSecret.Data[postgresCredentialsPasswordKey]
	if !ok {
		return fmt.Errorf("postgres credentials secret %s/%s is missing %q", stack.Name, secretName, postgresCredentialsPasswordKey)
	}

	_, _, err := core.CreateOrUpdate[*corev1.Secret](ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      getEncodedPostgresCredentialsSecretName(database),
	}, func(secret *corev1.Secret) error {
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{
			postgresCredentialsUsernameKey: []byte(escapePostgresCredentialForURI(string(username))),
			postgresCredentialsPasswordKey: []byte(escapePostgresCredentialForURI(string(password))),
		}
		return nil
	}, core.WithController[*corev1.Secret](ctx.GetScheme(), database))
	return errors.Wrap(err, "reconciling encoded postgres credentials secret")
}

func deleteEncodedPostgresCredentialsSecret(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database) error {
	err := core.DeleteIfExists[*corev1.Secret](ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      getEncodedPostgresCredentialsSecretName(database),
	})
	return errors.Wrap(err, "deleting encoded postgres credentials secret")
}
