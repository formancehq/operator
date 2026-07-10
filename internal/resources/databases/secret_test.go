package databases

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestReconcileEncodedPostgresCredentialsSecret(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		credentials      map[string][]byte
		encoding         postgresCredentialsEncoding
		expectedUsername string
		expectedPassword string
	}{
		{
			name: "encode raw credentials",
			credentials: map[string][]byte{
				postgresCredentialsUsernameKey: []byte("user^name"),
				postgresCredentialsPasswordKey: []byte("p^ss word"),
			},
			encoding:         postgresCredentialsEncodingRaw,
			expectedUsername: "user%5Ename",
			expectedPassword: "p%5Ess%20word",
		},
		{
			name: "preserve pre-encoded credentials",
			credentials: map[string][]byte{
				postgresCredentialsUsernameKey: []byte("user%5Ename"),
				postgresCredentialsPasswordKey: []byte("p%5Ess%20word"),
			},
			encoding:         postgresCredentialsEncodingURLEncoded,
			expectedUsername: "user%5Ename",
			expectedPassword: "p%5Ess%20word",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
				Data: tc.credentials,
			}
			ctx := newTestContext(t, sourceSecret)

			require.NoError(t, reconcileEncodedPostgresCredentialsSecret(
				ctx,
				stack,
				database,
				sourceSecret.Name,
				tc.encoding,
			))

			encodedSecret := &corev1.Secret{}
			require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
				Namespace: stack.Name,
				Name:      getEncodedPostgresCredentialsSecretName(database),
			}, encodedSecret))
			require.Equal(t, tc.expectedUsername, string(encodedSecret.Data[postgresCredentialsUsernameKey]))
			require.Equal(t, tc.expectedPassword, string(encodedSecret.Data[postgresCredentialsPasswordKey]))
			require.Equal(t, "true", encodedSecret.Annotations[encodedPostgresCredentialsSecretAnnotation])
			require.Len(t, encodedSecret.OwnerReferences, 1)
			require.Equal(t, database.Name, encodedSecret.OwnerReferences[0].Name)
		})
	}
}

func TestParsePostgresCredentialsEncoding(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		value    string
		expected postgresCredentialsEncoding
		wantErr  bool
	}{
		{
			name:     "defaults to url encoded",
			expected: postgresCredentialsEncodingURLEncoded,
		},
		{
			name:     "url encoded",
			value:    "urlEncoded",
			expected: postgresCredentialsEncodingURLEncoded,
		},
		{
			name:     "raw",
			value:    "raw",
			expected: postgresCredentialsEncodingRaw,
		},
		{
			name:    "invalid",
			value:   "automatic",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			actual, err := parsePostgresCredentialsEncoding(tc.value)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestReconcileEncodedPostgresCredentialsSecretAvoidsSourceSecretNameCollision(t *testing.T) {
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
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	ctx := newTestContext(t, sourceSecret)

	require.NoError(t, reconcileEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name, postgresCredentialsEncodingRaw))

	preservedSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      sourceSecret.Name,
	}, preservedSourceSecret))
	require.Equal(t, []byte("user^name"), preservedSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p^ss word"), preservedSourceSecret.Data[postgresCredentialsPasswordKey])

	encodedSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      getEncodedPostgresCredentialsSecretName(database, sourceSecret.Name),
	}, encodedSecret))
	require.NotEqual(t, sourceSecret.Name, encodedSecret.Name)
	require.Equal(t, []byte("user%5Ename"), encodedSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p%5Ess%20word"), encodedSecret.Data[postgresCredentialsPasswordKey])

	require.NoError(t, reconcileEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name, postgresCredentialsEncodingRaw))

	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      sourceSecret.Name,
	}, preservedSourceSecret))
	require.Equal(t, []byte("user^name"), preservedSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p^ss word"), preservedSourceSecret.Data[postgresCredentialsPasswordKey])

	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      encodedSecret.Name,
	}, encodedSecret))
	require.Equal(t, []byte("user%5Ename"), encodedSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p%5Ess%20word"), encodedSecret.Data[postgresCredentialsPasswordKey])
}

func TestReconcileEncodedPostgresCredentialsSecretKeepsUncontrolledLegacyFallbackSecret(t *testing.T) {
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
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	legacyFallbackSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      "stack-ledger-encoded-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("legacy-user"),
			postgresCredentialsPasswordKey: []byte("legacy-password"),
		},
	}
	ctx := newTestContext(t, sourceSecret, legacyFallbackSecret)

	require.NoError(t, reconcileEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name, postgresCredentialsEncodingRaw))

	preservedLegacySecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      legacyFallbackSecret.Name,
	}, preservedLegacySecret))
	require.Empty(t, preservedLegacySecret.OwnerReferences)
	require.Equal(t, []byte("legacy-user"), preservedLegacySecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("legacy-password"), preservedLegacySecret.Data[postgresCredentialsPasswordKey])

	encodedSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      getEncodedPostgresCredentialsSecretName(database, sourceSecret.Name),
	}, encodedSecret))
	require.NotEqual(t, sourceSecret.Name, encodedSecret.Name)
	require.NotEqual(t, legacyFallbackSecret.Name, encodedSecret.Name)
	require.Equal(t, []byte("user%5Ename"), encodedSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p%5Ess%20word"), encodedSecret.Data[postgresCredentialsPasswordKey])
}

func TestReconcileEncodedPostgresCredentialsSecretRejectsUncontrolledEncodedSecretTarget(t *testing.T) {
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
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	existingTargetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      getEncodedPostgresCredentialsSecretName(database, sourceSecret.Name),
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("existing-user"),
			postgresCredentialsPasswordKey: []byte("existing-password"),
		},
	}
	ctx := newTestContext(t, sourceSecret, existingTargetSecret)

	err := reconcileEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name, postgresCredentialsEncodingRaw)
	require.ErrorContains(t, err, "already exists and is not controlled")

	preservedTargetSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      existingTargetSecret.Name,
	}, preservedTargetSecret))
	require.Empty(t, preservedTargetSecret.OwnerReferences)
	require.Equal(t, []byte("existing-user"), preservedTargetSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("existing-password"), preservedTargetSecret.Data[postgresCredentialsPasswordKey])
}

func TestReconcileEncodedPostgresCredentialsSecretRemovesStaleSourceSecretControllerReference(t *testing.T) {
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
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, sourceSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, sourceSecret))

	require.NoError(t, reconcileEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name, postgresCredentialsEncodingRaw))

	preservedSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      sourceSecret.Name,
	}, preservedSourceSecret))
	require.Empty(t, preservedSourceSecret.OwnerReferences)
	require.Equal(t, []byte("user^name"), preservedSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p^ss word"), preservedSourceSecret.Data[postgresCredentialsPasswordKey])

	encodedSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      getEncodedPostgresCredentialsSecretName(database, sourceSecret.Name),
	}, encodedSecret))
	require.Equal(t, []byte("user%5Ename"), encodedSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p%5Ess%20word"), encodedSecret.Data[postgresCredentialsPasswordKey])
	require.Len(t, encodedSecret.OwnerReferences, 1)
	require.Equal(t, database.Name, encodedSecret.OwnerReferences[0].Name)
}

func TestReconcileEncodedPostgresCredentialsSecretDeletesStaleControlledEncodedSecret(t *testing.T) {
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
	staleEncodedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      "stack-ledger-encoded-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("old-user"),
			postgresCredentialsPasswordKey: []byte("old-password"),
		},
	}
	staleHashedEncodedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      getEncodedPostgresCredentialsSecretName(database, getEncodedPostgresCredentialsSecretName(database)),
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("old-hashed-user"),
			postgresCredentialsPasswordKey: []byte("old-hashed-password"),
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, staleEncodedSecret, ctx.GetScheme()))
	require.NoError(t, controllerutil.SetControllerReference(database, staleHashedEncodedSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, sourceSecret))
	require.NoError(t, ctx.GetClient().Create(ctx, staleEncodedSecret))
	require.NoError(t, ctx.GetClient().Create(ctx, staleHashedEncodedSecret))

	require.NoError(t, reconcileEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name, postgresCredentialsEncodingRaw))

	currentEncodedSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      getEncodedPostgresCredentialsSecretName(database, sourceSecret.Name),
	}, currentEncodedSecret))
	require.Equal(t, []byte("user%5Ename"), currentEncodedSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p%5Ess%20word"), currentEncodedSecret.Data[postgresCredentialsPasswordKey])

	err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      staleEncodedSecret.Name,
	}, &corev1.Secret{})
	require.True(t, apierrors.IsNotFound(err))

	err = ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      staleHashedEncodedSecret.Name,
	}, &corev1.Secret{})
	require.True(t, apierrors.IsNotFound(err))
}

func TestDeleteEncodedPostgresCredentialsSecretKeepsUncontrolledSourceSecret(t *testing.T) {
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
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	ctx := newTestContext(t, sourceSecret)

	require.NoError(t, deleteEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name))

	preservedSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      sourceSecret.Name,
	}, preservedSourceSecret))
	require.Equal(t, []byte("user^name"), preservedSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p^ss word"), preservedSourceSecret.Data[postgresCredentialsPasswordKey])
}

func TestDeleteEncodedPostgresCredentialsSecretRemovesStaleSourceSecretControllerReference(t *testing.T) {
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
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, sourceSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, sourceSecret))

	require.NoError(t, deleteEncodedPostgresCredentialsSecret(ctx, stack, database, sourceSecret.Name))

	preservedSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      sourceSecret.Name,
	}, preservedSourceSecret))
	require.Empty(t, preservedSourceSecret.OwnerReferences)
	require.Equal(t, []byte("user^name"), preservedSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p^ss word"), preservedSourceSecret.Data[postgresCredentialsPasswordKey])
}

func TestDeleteEncodedPostgresCredentialsSecretPreservesDefaultCandidateWhenSourceReferenceIsMissing(t *testing.T) {
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
	possibleSourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, possibleSourceSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, possibleSourceSecret))

	require.NoError(t, deleteEncodedPostgresCredentialsSecret(ctx, stack, database, ""))

	preservedSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      possibleSourceSecret.Name,
	}, preservedSourceSecret))
	require.Empty(t, preservedSourceSecret.OwnerReferences)
	require.Equal(t, []byte("user^name"), preservedSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p^ss word"), preservedSourceSecret.Data[postgresCredentialsPasswordKey])
}

func TestDeleteRemovesAnnotatedEncodedSecretWhenSourceReferenceAndStatusURIAreMissing(t *testing.T) {
	t.Parallel()

	database := &v1beta1.Database{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       "Database",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger",
			UID:  types.UID("database-uid"),
		},
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{
				Stack: "stack",
			},
		},
	}
	encodedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Spec.Stack,
			Name:      getEncodedPostgresCredentialsSecretName(database),
			Annotations: map[string]string{
				encodedPostgresCredentialsSecretAnnotation: "true",
			},
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user%5Ename"),
			postgresCredentialsPasswordKey: []byte("p%5Ess%20word"),
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, encodedSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, encodedSecret))

	require.NoError(t, Delete(ctx, database))

	err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: database.Spec.Stack,
		Name:      encodedSecret.Name,
	}, &corev1.Secret{})
	require.True(t, apierrors.IsNotFound(err))
}

func TestDeleteKeepsUnannotatedDefaultSecretOwnerReferenceWhenSourceReferenceAndStatusURIAreMissing(t *testing.T) {
	t.Parallel()

	database := &v1beta1.Database{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       "Database",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger",
			UID:  types.UID("database-uid"),
		},
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{
				Stack: "stack",
			},
		},
	}
	defaultSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Spec.Stack,
			Name:      getEncodedPostgresCredentialsSecretName(database),
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user"),
			postgresCredentialsPasswordKey: []byte("password"),
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, defaultSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, defaultSecret))

	require.NoError(t, Delete(ctx, database))

	preservedDefaultSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: database.Spec.Stack,
		Name:      defaultSecret.Name,
	}, preservedDefaultSecret))
	require.True(t, metav1.IsControlledBy(preservedDefaultSecret, database))
}

func TestDeleteEncodedPostgresCredentialsSecretDeletesControlledEncodedSecretWithKnownSource(t *testing.T) {
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
	encodedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user%5Ename"),
			postgresCredentialsPasswordKey: []byte("p%5Ess%20word"),
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, encodedSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, encodedSecret))

	require.NoError(t, deleteEncodedPostgresCredentialsSecret(ctx, stack, database, "postgres"))

	err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      encodedSecret.Name,
	}, &corev1.Secret{})
	require.True(t, apierrors.IsNotFound(err))
}

func TestReconcileKeepsPostgresResourceReferenceWhenEncodedSecretDeleteFails(t *testing.T) {
	t.Parallel()

	oldPostgresURI, err := v1beta1.ParseURL("postgresql://postgres:5432?secret=postgres")
	require.NoError(t, err)
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
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{
				Stack: stack.Name,
			},
			Service: "ledger",
		},
		Status: v1beta1.DatabaseStatus{
			Status: v1beta1.Status{
				Ready: true,
			},
			URI:      oldPostgresURI,
			Database: "ledger",
		},
	}
	postgresURISetting := &v1beta1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres-uri"},
		Spec: v1beta1.SettingsSpec{
			Stacks: []string{stack.Name},
			Key:    "postgres.ledger.uri",
			Value:  "postgresql://postgres:5432",
		},
	}
	resourceReference := &v1beta1.ResourceReference{
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger-postgres",
		},
		Spec: v1beta1.ResourceReferenceSpec{
			Name: "postgres",
		},
	}
	encodedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      getEncodedPostgresCredentialsSecretName(database),
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user%5Ename"),
			postgresCredentialsPasswordKey: []byte("p%5Ess%20word"),
		},
	}
	ctx := newTestContextWithInterceptor(t, interceptor.Funcs{
		Delete: func(ctx context.Context, interceptedClient client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetNamespace() == encodedSecret.Namespace && obj.GetName() == encodedSecret.Name {
				return errors.New("delete encoded postgres credentials secret failed")
			}
			return interceptedClient.Delete(ctx, obj, opts...)
		},
	}, stack, postgresURISetting, resourceReference)
	require.NoError(t, controllerutil.SetControllerReference(database, encodedSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, encodedSecret))

	err = Reconcile(ctx, stack, database)
	require.ErrorContains(t, err, "delete encoded postgres credentials secret failed")

	preservedResourceReference := &v1beta1.ResourceReference{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Name: resourceReference.Name,
	}, preservedResourceReference))
	require.Equal(t, "postgres", preservedResourceReference.Spec.Name)

	preservedEncodedSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      encodedSecret.Name,
	}, preservedEncodedSecret))
}

func TestReconcileEncodedPostgresCredentialsSecretBeforeDatabaseDeleteCreatesCollisionSafeSecret(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack"},
	}
	databaseURI, err := v1beta1.ParseURL("postgresql://postgres:5432?secret=stack-ledger-postgres-uri-credentials&secretCredentialsEncoding=raw")
	require.NoError(t, err)
	database := &v1beta1.Database{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       "Database",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger",
			UID:  types.UID("database-uid"),
		},
		Status: v1beta1.DatabaseStatus{
			URI: databaseURI,
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: stack.Name,
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	ctx := newTestContext(t, sourceSecret)

	require.NoError(t, reconcileEncodedPostgresCredentialsSecretBeforeDatabaseDelete(ctx, stack, database))

	encodedSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: stack.Name,
		Name:      getEncodedPostgresCredentialsSecretName(database, sourceSecret.Name),
	}, encodedSecret))
	require.NotEqual(t, sourceSecret.Name, encodedSecret.Name)
	require.Equal(t, "true", encodedSecret.Annotations[encodedPostgresCredentialsSecretAnnotation])
	require.Equal(t, []byte("user%5Ename"), encodedSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p%5Ess%20word"), encodedSecret.Data[postgresCredentialsPasswordKey])
}

func TestDeleteRemediatesStaleSourceSecretControllerReferenceBeforeClearDatabaseCheck(t *testing.T) {
	t.Parallel()

	postgresURI, err := url.Parse("postgresql://postgres:5432?secret=stack-ledger-postgres-uri-credentials")
	require.NoError(t, err)
	database := &v1beta1.Database{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       "Database",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger",
			UID:  types.UID("database-uid"),
		},
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{
				Stack: "stack",
			},
		},
		Status: v1beta1.DatabaseStatus{
			URI: &v1beta1.URI{URL: postgresURI},
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Spec.Stack,
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, sourceSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, sourceSecret))

	require.NoError(t, Delete(ctx, database))

	preservedSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: database.Spec.Stack,
		Name:      sourceSecret.Name,
	}, preservedSourceSecret))
	require.Empty(t, preservedSourceSecret.OwnerReferences)
	require.Equal(t, []byte("user^name"), preservedSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p^ss word"), preservedSourceSecret.Data[postgresCredentialsPasswordKey])
}

func TestRemediatePostgresCredentialsSecretBeforeDatabaseDeleteUsesResourceReference(t *testing.T) {
	t.Parallel()

	database := &v1beta1.Database{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       "Database",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger",
			UID:  types.UID("database-uid"),
		},
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{
				Stack: "stack",
			},
		},
	}
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Spec.Stack,
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("user^name"),
			postgresCredentialsPasswordKey: []byte("p^ss word"),
		},
	}
	resourceReference := &v1beta1.ResourceReference{
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger-postgres",
		},
		Spec: v1beta1.ResourceReferenceSpec{
			Name: sourceSecret.Name,
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, sourceSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, sourceSecret))
	require.NoError(t, ctx.GetClient().Create(ctx, resourceReference))

	require.NoError(t, remediatePostgresCredentialsSecretBeforeDatabaseDelete(ctx, database))

	preservedSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: database.Spec.Stack,
		Name:      sourceSecret.Name,
	}, preservedSourceSecret))
	require.Empty(t, preservedSourceSecret.OwnerReferences)
	require.Equal(t, []byte("user^name"), preservedSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("p^ss word"), preservedSourceSecret.Data[postgresCredentialsPasswordKey])
}

func TestRemediatePostgresCredentialsSecretBeforeDatabaseDeleteUsesResourceReferenceAndStatusURI(t *testing.T) {
	t.Parallel()

	postgresURI, err := url.Parse("postgresql://postgres:5432?secret=stack-ledger-postgres-uri-credentials")
	require.NoError(t, err)
	database := &v1beta1.Database{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       "Database",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger",
			UID:  types.UID("database-uid"),
		},
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{
				Stack: "stack",
			},
		},
		Status: v1beta1.DatabaseStatus{
			URI: &v1beta1.URI{URL: postgresURI},
		},
	}
	statusSourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Spec.Stack,
			Name:      "stack-ledger-postgres-uri-credentials",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("old-user"),
			postgresCredentialsPasswordKey: []byte("old-password"),
		},
	}
	referenceSourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Spec.Stack,
			Name:      "postgres-current",
		},
		Data: map[string][]byte{
			postgresCredentialsUsernameKey: []byte("current-user"),
			postgresCredentialsPasswordKey: []byte("current-password"),
		},
	}
	resourceReference := &v1beta1.ResourceReference{
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack-ledger-postgres",
		},
		Spec: v1beta1.ResourceReferenceSpec{
			Name: referenceSourceSecret.Name,
		},
	}
	ctx := newTestContext(t)
	require.NoError(t, controllerutil.SetControllerReference(database, statusSourceSecret, ctx.GetScheme()))
	require.NoError(t, controllerutil.SetControllerReference(database, referenceSourceSecret, ctx.GetScheme()))
	require.NoError(t, ctx.GetClient().Create(ctx, statusSourceSecret))
	require.NoError(t, ctx.GetClient().Create(ctx, referenceSourceSecret))
	require.NoError(t, ctx.GetClient().Create(ctx, resourceReference))

	require.NoError(t, remediatePostgresCredentialsSecretBeforeDatabaseDelete(ctx, database))

	preservedStatusSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: database.Spec.Stack,
		Name:      statusSourceSecret.Name,
	}, preservedStatusSourceSecret))
	require.Empty(t, preservedStatusSourceSecret.OwnerReferences)
	require.Equal(t, []byte("old-user"), preservedStatusSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("old-password"), preservedStatusSourceSecret.Data[postgresCredentialsPasswordKey])

	preservedReferenceSourceSecret := &corev1.Secret{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: database.Spec.Stack,
		Name:      referenceSourceSecret.Name,
	}, preservedReferenceSourceSecret))
	require.Empty(t, preservedReferenceSourceSecret.OwnerReferences)
	require.Equal(t, []byte("current-user"), preservedReferenceSourceSecret.Data[postgresCredentialsUsernameKey])
	require.Equal(t, []byte("current-password"), preservedReferenceSourceSecret.Data[postgresCredentialsPasswordKey])
}
