package databases

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
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
