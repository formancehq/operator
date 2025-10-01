package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsGreaterOrEqual(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		version  string
		than     string
		expected bool
	}{
		// Valid semver comparisons
		{
			name:     "equal versions",
			version:  "v1.2.3",
			than:     "v1.2.3",
			expected: true,
		},
		{
			name:     "greater major version",
			version:  "v2.0.0",
			than:     "v1.2.3",
			expected: true,
		},
		{
			name:     "greater minor version",
			version:  "v1.3.0",
			than:     "v1.2.3",
			expected: true,
		},
		{
			name:     "greater patch version",
			version:  "v1.2.4",
			than:     "v1.2.3",
			expected: true,
		},
		{
			name:     "lower version",
			version:  "v1.2.0",
			than:     "v1.2.3",
			expected: false,
		},
		// Invalid semver cases (like "latest", "main", etc.)
		{
			name:     "latest vs valid semver",
			version:  "latest",
			than:     "v1.2.3",
			expected: true,
		},
		{
			name:     "valid semver vs latest",
			version:  "v1.2.3",
			than:     "latest",
			expected: false,
		},
		{
			name:     "both invalid semver",
			version:  "latest",
			than:     "main",
			expected: true,
		},
		{
			name:     "branch name vs semver",
			version:  "main",
			than:     "v1.2.3",
			expected: true,
		},
		{
			name:     "semver vs branch name",
			version:  "v1.2.3",
			than:     "main",
			expected: false,
		},
		// Edge cases
		{
			name:     "v0.0.0 vs v0.0.1",
			version:  "v0.0.0",
			than:     "v0.0.1",
			expected: false,
		},
		{
			name:     "equal v0.0.0",
			version:  "v0.0.0",
			than:     "v0.0.0",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsGreaterOrEqual(tt.version, tt.than)
			require.Equal(t, tt.expected, result,
				"IsGreaterOrEqual(%q, %q) = %v, expected %v",
				tt.version, tt.than, result, tt.expected)
		})
	}
}

func TestIsLower(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		version  string
		than     string
		expected bool
	}{
		// Valid semver comparisons
		{
			name:     "equal versions",
			version:  "v1.2.3",
			than:     "v1.2.3",
			expected: false,
		},
		{
			name:     "lower major version",
			version:  "v1.2.3",
			than:     "v2.0.0",
			expected: true,
		},
		{
			name:     "lower minor version",
			version:  "v1.2.3",
			than:     "v1.3.0",
			expected: true,
		},
		{
			name:     "lower patch version",
			version:  "v1.2.3",
			than:     "v1.2.4",
			expected: true,
		},
		{
			name:     "greater version",
			version:  "v1.3.0",
			than:     "v1.2.3",
			expected: false,
		},
		// Invalid semver cases
		{
			name:     "latest vs valid semver",
			version:  "latest",
			than:     "v1.2.3",
			expected: false,
		},
		{
			name:     "valid semver vs latest",
			version:  "v1.2.3",
			than:     "latest",
			expected: true,
		},
		{
			name:     "both invalid semver",
			version:  "latest",
			than:     "main",
			expected: false,
		},
		{
			name:     "branch name vs semver",
			version:  "main",
			than:     "v1.2.3",
			expected: false,
		},
		{
			name:     "semver vs branch name",
			version:  "v1.2.3",
			than:     "main",
			expected: true,
		},
		// Edge cases
		{
			name:     "v0.0.0 vs v0.0.1",
			version:  "v0.0.0",
			than:     "v0.0.1",
			expected: true,
		},
		{
			name:     "v0.0.1 vs v0.0.0",
			version:  "v0.0.1",
			than:     "v0.0.0",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsLower(tt.version, tt.than)
			require.Equal(t, tt.expected, result,
				"IsLower(%q, %q) = %v, expected %v",
				tt.version, tt.than, result, tt.expected)
		})
	}
}

// TestVersionComparisonConsistency ensures IsGreaterOrEqual and IsLower are consistent
func TestVersionComparisonConsistency(t *testing.T) {
	t.Parallel()
	versions := []string{
		"v0.0.1",
		"v1.0.0",
		"v1.2.3",
		"v2.0.0",
		"latest",
		"main",
	}

	for _, v1 := range versions {
		for _, v2 := range versions {
			t.Run(v1+"_vs_"+v2, func(t *testing.T) {
				gte := IsGreaterOrEqual(v1, v2)
				lt := IsLower(v1, v2)

				// If v1 == v2, then gte should be true and lt should be false
				if v1 == v2 {
					require.True(t, gte, "%s >= %s should be true", v1, v2)
					require.False(t, lt, "%s < %s should be false", v1, v2)
				} else {
					// If v1 < v2, then lt should be true and gte should be false
					// If v1 >= v2, then gte should be true and lt should be false
					// These should never both be true or both be false (except for equal)
					require.NotEqual(t, gte, lt,
						"Inconsistent comparison for %s vs %s: gte=%v, lt=%v",
						v1, v2, gte, lt)
				}
			})
		}
	}
}