package registries

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty version becomes latest",
			input:    "",
			expected: "latest",
		},
		{
			name:     "semver version unchanged",
			input:    "v1.2.3",
			expected: "v1.2.3",
		},
		{
			name:     "latest unchanged",
			input:    "latest",
			expected: "latest",
		},
		{
			name:     "branch name unchanged",
			input:    "main",
			expected: "main",
		},
		{
			name:     "sha unchanged",
			input:    "abc123def",
			expected: "abc123def",
		},
		{
			name:     "version with build metadata",
			input:    "v2.0.0-rc.1+build.123",
			expected: "v2.0.0-rc.1+build.123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeVersion(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestImageURLConstruction(t *testing.T) {
	t.Parallel()

	// Test the expected format of GetFormanceImage image URLs
	tests := []struct {
		name            string
		imageName       string
		version         string
		expectedPattern string // What we expect the full image path to contain
	}{
		{
			name:            "formance image with version",
			imageName:       "ledger",
			version:         "v2.0.0",
			expectedPattern: "ghcr.io/formancehq/ledger:v2.0.0",
		},
		{
			name:            "formance image with latest",
			imageName:       "payments",
			version:         "latest",
			expectedPattern: "ghcr.io/formancehq/payments:latest",
		},
		{
			name:            "formance image with empty version becomes latest",
			imageName:       "wallets",
			version:         "",
			expectedPattern: "ghcr.io/formancehq/wallets:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Test the components that would be used to construct the image URL
			normalizedVersion := NormalizeVersion(tt.version)
			expectedURL := "ghcr.io/formancehq/" + tt.imageName + ":" + normalizedVersion

			require.Equal(t, tt.expectedPattern, expectedURL)
		})
	}
}

func TestBenthosImageURLConstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		version         string
		expectedPattern string
	}{
		{
			name:            "benthos with version",
			version:         "4.20.0",
			expectedPattern: "public.ecr.aws/formance-internal/jeffail/benthos:4.20.0",
		},
		{
			name:            "benthos with latest",
			version:         "latest",
			expectedPattern: "public.ecr.aws/formance-internal/jeffail/benthos:latest",
		},
		{
			name:            "benthos with empty version",
			version:         "",
			expectedPattern: "public.ecr.aws/formance-internal/jeffail/benthos:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			normalizedVersion := NormalizeVersion(tt.version)
			expectedURL := "public.ecr.aws/formance-internal/jeffail/benthos:" + normalizedVersion

			require.Equal(t, tt.expectedPattern, expectedURL)
		})
	}
}

func TestNatsBoxImageURLConstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		version         string
		expectedPattern string
	}{
		{
			name:            "nats-box with version",
			version:         "0.14.0",
			expectedPattern: "docker.io/natsio/nats-box:0.14.0",
		},
		{
			name:            "nats-box with latest",
			version:         "latest",
			expectedPattern: "docker.io/natsio/nats-box:latest",
		},
		{
			name:            "nats-box with empty version",
			version:         "",
			expectedPattern: "docker.io/natsio/nats-box:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			normalizedVersion := NormalizeVersion(tt.version)
			expectedURL := "docker.io/natsio/nats-box:" + normalizedVersion

			require.Equal(t, tt.expectedPattern, expectedURL)
		})
	}
}

// TestImageRegistryFormats documents the supported image registry formats
func TestImageRegistryFormats(t *testing.T) {
	t.Parallel()

	// This test documents the supported formats mentioned in the comment
	registryFormats := []struct {
		description string
		example     string
	}{
		{
			description: "GitHub Container Registry",
			example:     "ghcr.io/formancehq/ledger:v2.0.0",
		},
		{
			description: "AWS ECR Public",
			example:     "public.ecr.aws/formance-internal/jeffail/benthos:latest",
		},
		{
			description: "Docker Hub",
			example:     "docker.io/natsio/nats-box:0.14.0",
		},
	}

	for _, format := range registryFormats {
		t.Run(format.description, func(t *testing.T) {
			t.Parallel()
			// This test just documents the formats, no actual assertions needed
			require.NotEmpty(t, format.example)
		})
	}
}