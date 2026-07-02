package databases

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func TestBuildPostgresQueryString(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name          string
		uri           string
		expectedQuery string // expected query string (without leading '?')
	}

	testCases := []testCase{
		{
			name:          "no query params",
			uri:           "postgresql://user:pass@host:5432",
			expectedQuery: "",
		},
		{
			name:          "disableSSLMode=true produces sslmode=disable",
			uri:           "postgresql://user:pass@host:5432?disableSSLMode=true",
			expectedQuery: "sslmode=disable",
		},
		{
			name:          "disableSSLMode=false is stripped with no sslmode",
			uri:           "postgresql://user:pass@host:5432?disableSSLMode=false",
			expectedQuery: "",
		},
		{
			name:          "custom sslmode is preserved",
			uri:           "postgresql://user:pass@host:5432?sslmode=require",
			expectedQuery: "sslmode=require",
		},
		{
			name:          "multiple custom params are preserved",
			uri:           "postgresql://user:pass@host:5432?sslmode=require&tcpKeepAlive=true",
			expectedQuery: "sslmode=require&tcpKeepAlive=true",
		},
		{
			name:          "secret is filtered out",
			uri:           "postgresql://host:5432?secret=creds&sslmode=require",
			expectedQuery: "sslmode=require",
		},
		{
			name:          "disableSSLMode=true overrides existing sslmode",
			uri:           "postgresql://user:pass@host:5432?disableSSLMode=true&sslmode=require",
			expectedQuery: "sslmode=disable",
		},
		{
			name:          "awsRole is filtered out",
			uri:           "postgresql://user:pass@host:5432?awsRole=my-role&sslmode=require",
			expectedQuery: "sslmode=require",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsedURI, err := url.Parse(tc.uri)
			require.NoError(t, err)

			actual := BuildPostgresQueryString(parsedURI.Query())

			if tc.expectedQuery == "" {
				require.Empty(t, actual, "expected no query string, got: %s", actual)
			} else {
				expectedParams, err := url.ParseQuery(tc.expectedQuery)
				require.NoError(t, err)
				actualParams, err := url.ParseQuery(actual)
				require.NoError(t, err)
				require.Equal(t, expectedParams, actualParams)
			}
		})
	}
}

type testContext struct {
	context.Context
	client    client.Client
	apiReader client.Reader
	scheme    *runtime.Scheme
}

func (t testContext) GetClient() client.Client {
	return t.client
}

func (t testContext) GetScheme() *runtime.Scheme {
	return t.scheme
}

func (t testContext) GetAPIReader() client.Reader {
	return t.apiReader
}

func (t testContext) GetPlatform() core.Platform {
	return core.Platform{}
}

func newTestContext(t *testing.T, objects ...client.Object) testContext {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
			settings := obj.(*v1beta1.Settings)
			return settings.Spec.Stacks
		}).
		WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
			settings := obj.(*v1beta1.Settings)
			keys := strings.Split(settings.Spec.Key, ".")
			return []string{fmt.Sprint(len(keys))}
		}).
		Build()

	return testContext{
		Context:   context.Background(),
		client:    fakeClient,
		apiReader: fakeClient,
		scheme:    scheme,
	}
}

func TestGetPostgresEnvVarsUsesEncodedSecretForURI(t *testing.T) {
	t.Parallel()

	ctx := newTestContext(t)
	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack"},
	}
	postgresURI, err := v1beta1.ParseURL("postgresql://postgres:5432?secret=postgres")
	require.NoError(t, err)
	database := &v1beta1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: "stack-ledger"},
		Spec: v1beta1.DatabaseSpec{
			Service: "ledger",
		},
		Status: v1beta1.DatabaseStatus{
			URI:      postgresURI,
			Database: "ledger",
		},
	}

	envVars, err := GetPostgresEnvVars(ctx, stack, database)
	require.NoError(t, err)

	envByName := make(map[string]corev1.EnvVar, len(envVars))
	for _, envVar := range envVars {
		envByName[envVar.Name] = envVar
	}

	require.Equal(t, "postgres", envByName["POSTGRES_USERNAME"].ValueFrom.SecretKeyRef.Name)
	require.Equal(t, postgresCredentialsUsernameKey, envByName["POSTGRES_USERNAME"].ValueFrom.SecretKeyRef.Key)
	require.Equal(t, "postgres", envByName["POSTGRES_PASSWORD"].ValueFrom.SecretKeyRef.Name)
	require.Equal(t, postgresCredentialsPasswordKey, envByName["POSTGRES_PASSWORD"].ValueFrom.SecretKeyRef.Key)

	encodedSecretName := getEncodedPostgresCredentialsSecretName(database)
	require.Equal(t, encodedSecretName, envByName["POSTGRES_URL_ENCODED_USERNAME"].ValueFrom.SecretKeyRef.Name)
	require.Equal(t, postgresCredentialsUsernameKey, envByName["POSTGRES_URL_ENCODED_USERNAME"].ValueFrom.SecretKeyRef.Key)
	require.Equal(t, encodedSecretName, envByName["POSTGRES_URL_ENCODED_PASSWORD"].ValueFrom.SecretKeyRef.Name)
	require.Equal(t, postgresCredentialsPasswordKey, envByName["POSTGRES_URL_ENCODED_PASSWORD"].ValueFrom.SecretKeyRef.Key)

	require.Equal(t,
		"postgresql://$(POSTGRES_URL_ENCODED_USERNAME):$(POSTGRES_URL_ENCODED_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)",
		envByName["POSTGRES_NO_DATABASE_URI"].Value,
	)
	require.Equal(t, "$(POSTGRES_NO_DATABASE_URI)/$(POSTGRES_DATABASE)", envByName["POSTGRES_URI"].Value)
}

func TestGetPostgresEnvVarsEscapesInlineCredentialsForURIUserinfo(t *testing.T) {
	t.Parallel()

	ctx := newTestContext(t)
	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack"},
	}
	postgresURI, err := v1beta1.ParseURL("postgresql://user%20name:p%5Ess%20word@postgres:5432")
	require.NoError(t, err)
	database := &v1beta1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: "stack-ledger"},
		Spec: v1beta1.DatabaseSpec{
			Service: "ledger",
		},
		Status: v1beta1.DatabaseStatus{
			URI:      postgresURI,
			Database: "ledger",
		},
	}

	envVars, err := GetPostgresEnvVars(ctx, stack, database)
	require.NoError(t, err)

	envByName := make(map[string]corev1.EnvVar, len(envVars))
	for _, envVar := range envVars {
		envByName[envVar.Name] = envVar
	}

	require.Equal(t, "user%20name", envByName["POSTGRES_USERNAME"].Value)
	require.Equal(t, "p%5Ess%20word", envByName["POSTGRES_PASSWORD"].Value)
	require.Equal(t,
		"postgresql://$(POSTGRES_USERNAME):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)",
		envByName["POSTGRES_NO_DATABASE_URI"].Value,
	)
}

func TestEscapePostgresCredentialForURI(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		credential string
		expected   string
	}{
		{
			name:       "space",
			credential: "p ss",
			expected:   "p%20ss",
		},
		{
			name:       "plus",
			credential: "p+ss",
			expected:   "p+ss",
		},
		{
			name:       "authority delimiters",
			credential: "p@ss:word",
			expected:   "p%40ss%3Aword",
		},
		{
			name:       "caret",
			credential: "p^ss",
			expected:   "p%5Ess",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.expected, escapePostgresCredentialForURI(tc.credential))
		})
	}
}
