package databases

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestReconcileEncodedPostgresCredentialsSecret(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack"},
	}
	database := &v1beta1.Database{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       "Database",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger",
			UID:  types.UID("database-uid"),
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      "postgres",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	ctx := newTestContext(t, sourceSecret)

	require.NoError(t, reconcileEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name))

	encodedSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      getEncodedPostgresCredentialsSecretName(database),
	}, encodedSecret))
	require.Equal(t, []byte("user%5Ename"), encodedSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p%5Ess%20word"), encodedSecret.Data[postgresCredentialsPasswordKey])
	require.Len(t, encodedSecret.OwnerReferences, 1)
	require.Equal(t, database.Name, encodedSecret.OwnerReferences[0].Name)
}
