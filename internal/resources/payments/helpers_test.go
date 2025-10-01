package payments

import (
	"net/url"
	"testing"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/stretchr/testify/require"
)

func TestValidateTemporalURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		uriString string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid temporal URI",
			uriString: "temporal://localhost:7233/default",
			wantError: false,
		},
		{
			name:      "valid temporal URI with port",
			uriString: "temporal://temporal.namespace:7233/my-namespace",
			wantError: false,
		},
		{
			name:      "valid temporal URI with query params",
			uriString: "temporal://localhost:7233/default?tls=true",
			wantError: false,
		},
		{
			name:      "invalid scheme - http",
			uriString: "http://localhost:7233/default",
			wantError: true,
			errorMsg:  "invalid temporal uri",
		},
		{
			name:      "invalid scheme - https",
			uriString: "https://localhost:7233/default",
			wantError: true,
			errorMsg:  "invalid temporal uri",
		},
		{
			name:      "invalid scheme - grpc",
			uriString: "grpc://localhost:7233/default",
			wantError: true,
			errorMsg:  "invalid temporal uri",
		},
		{
			name:      "missing path",
			uriString: "temporal://localhost:7233",
			wantError: true,
			errorMsg:  "invalid temporal uri",
		},
		{
			name:      "path with multiple segments",
			uriString: "temporal://localhost:7233/team/namespace",
			wantError: false,
		},
		{
			name:      "localhost without port",
			uriString: "temporal://localhost/default",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsedURL, err := url.Parse(tt.uriString)
			require.NoError(t, err, "URL should parse correctly")

			temporalURI := &v1beta1.URI{URL: parsedURL}

			err = validateTemporalURI(temporalURI)

			if tt.wantError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					require.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateTemporalURIScheme(t *testing.T) {
	t.Parallel()

	// Test that only "temporal" scheme is accepted
	validSchemes := []string{"temporal"}
	invalidSchemes := []string{"http", "https", "grpc", "tcp", "udp", ""}

	for _, scheme := range validSchemes {
		t.Run("valid_scheme_"+scheme, func(t *testing.T) {
			t.Parallel()

			parsedURL, _ := url.Parse(scheme + "://localhost:7233/default")
			temporalURI := &v1beta1.URI{URL: parsedURL}

			err := validateTemporalURI(temporalURI)
			require.NoError(t, err, "Scheme %s should be valid", scheme)
		})
	}

	for _, scheme := range invalidSchemes {
		t.Run("invalid_scheme_"+scheme, func(t *testing.T) {
			t.Parallel()

			var uriString string
			if scheme == "" {
				uriString = "//localhost:7233/default"
			} else {
				uriString = scheme + "://localhost:7233/default"
			}

			parsedURL, _ := url.Parse(uriString)
			temporalURI := &v1beta1.URI{URL: parsedURL}

			err := validateTemporalURI(temporalURI)
			require.Error(t, err, "Scheme %s should be invalid", scheme)
			require.Contains(t, err.Error(), "invalid temporal uri")
		})
	}
}

func TestValidateTemporalURIPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		wantError bool
	}{
		{
			name:      "valid path with namespace",
			path:      "/default",
			wantError: false,
		},
		{
			name:      "valid path with custom namespace",
			path:      "/my-namespace",
			wantError: false,
		},
		{
			name:      "valid path with multiple segments",
			path:      "/team/namespace",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Construct a temporal URI with the test path
			baseURL := "temporal://localhost:7233"
			parsedURL, _ := url.Parse(baseURL + tt.path)
			temporalURI := &v1beta1.URI{URL: parsedURL}

			err := validateTemporalURI(temporalURI)

			if tt.wantError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid temporal uri")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateTemporalURIErrorMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		uriString    string
		expectedMsg  string
	}{
		{
			name:        "invalid scheme includes URI in error",
			uriString:   "http://localhost:7233/default",
			expectedMsg: "http://localhost:7233/default",
		},
		{
			name:        "missing path includes URI in error",
			uriString:   "temporal://localhost:7233",
			expectedMsg: "temporal://localhost:7233",
		},
		{
			name:        "path without slash includes URI in error",
			uriString:   "temporal://localhost:7233",
			expectedMsg: "temporal://localhost:7233",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsedURL, _ := url.Parse(tt.uriString)
			temporalURI := &v1beta1.URI{URL: parsedURL}

			err := validateTemporalURI(temporalURI)

			require.Error(t, err)
			require.Contains(t, err.Error(), tt.expectedMsg,
				"Error message should include the invalid URI")
		})
	}
}

func TestValidateTemporalURIWithQueryParameters(t *testing.T) {
	t.Parallel()

	// Query parameters should not affect validation
	tests := []struct {
		name      string
		uriString string
	}{
		{
			name:      "with tls parameter",
			uriString: "temporal://localhost:7233/default?tls=true",
		},
		{
			name:      "with secret parameter",
			uriString: "temporal://localhost:7233/default?secret=my-secret",
		},
		{
			name:      "with multiple parameters",
			uriString: "temporal://localhost:7233/default?tls=true&secret=my-secret&timeout=30s",
		},
		{
			name:      "with encryptionKeySecret parameter",
			uriString: "temporal://localhost:7233/default?encryptionKeySecret=encryption-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsedURL, _ := url.Parse(tt.uriString)
			temporalURI := &v1beta1.URI{URL: parsedURL}

			err := validateTemporalURI(temporalURI)
			require.NoError(t, err, "Query parameters should not affect validation")
		})
	}
}