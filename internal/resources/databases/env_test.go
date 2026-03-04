package databases

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func TestGetPostgresEnvVars(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name          string
		uri           string
		expectedQuery string // expected query string in POSTGRES_URI (without leading '?')
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			require.NoError(t, v1beta1.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
					return obj.(*v1beta1.Settings).GetStacks()
				}).
				WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
					key := obj.(*v1beta1.Settings).Spec.Key
					keyParts := settings.SplitKeywordWithDot(key)
					return []string{fmt.Sprintf("%d", len(keyParts))}
				}).
				Build()

			mockCtx := &mockContext{
				Context: context.Background(),
				client:  fakeClient,
				scheme:  scheme,
			}

			parsedURI, err := url.Parse(tc.uri)
			require.NoError(t, err)

			stack := &v1beta1.Stack{}
			stack.Name = "test-stack"

			database := &v1beta1.Database{}
			database.Spec.Service = "ledger"
			database.Status.URI = &v1beta1.URI{URL: parsedURI}
			database.Status.Database = "testdb"

			envVars, err := GetPostgresEnvVars(mockCtx, stack, database)
			require.NoError(t, err)

			// Find POSTGRES_URI env var
			var postgresURI string
			for _, env := range envVars {
				if env.Name == "POSTGRES_URI" {
					postgresURI = env.Value
					break
				}
			}
			require.NotEmpty(t, postgresURI, "POSTGRES_URI env var should be present")

			// POSTGRES_URI is built with ComputeEnvVar, so it has the format:
			// $(POSTGRES_NO_DATABASE_URI)/$(POSTGRES_DATABASE)?query_params
			// We need to extract the query string part after the database placeholder.
			base := "$(POSTGRES_NO_DATABASE_URI)/$(POSTGRES_DATABASE)"
			require.True(t, strings.HasPrefix(postgresURI, base),
				"POSTGRES_URI should start with %q, got: %s", base, postgresURI)

			suffix := strings.TrimPrefix(postgresURI, base)
			if tc.expectedQuery == "" {
				require.Empty(t, suffix, "expected no query string, got: %s", suffix)
			} else {
				require.True(t, strings.HasPrefix(suffix, "?"),
					"expected query string to start with '?', got: %s", suffix)
				actualQuery := strings.TrimPrefix(suffix, "?")

				// Parse both to compare regardless of key ordering
				expectedParams, err := url.ParseQuery(tc.expectedQuery)
				require.NoError(t, err)
				actualParams, err := url.ParseQuery(actualQuery)
				require.NoError(t, err)
				require.Equal(t, expectedParams, actualParams)
			}
		})
	}
}
