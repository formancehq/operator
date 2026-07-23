package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestMergeEnvVars(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name      string
		base      []corev1.EnvVar
		overrides []corev1.EnvVar
		expected  []corev1.EnvVar
	}

	testCases := []testCase{
		{
			name:      "empty overrides returns base unchanged",
			base:      []corev1.EnvVar{{Name: "A", Value: "1"}},
			overrides: nil,
			expected:  []corev1.EnvVar{{Name: "A", Value: "1"}},
		},
		{
			name:      "empty base returns overrides",
			base:      nil,
			overrides: []corev1.EnvVar{{Name: "A", Value: "1"}},
			expected:  []corev1.EnvVar{{Name: "A", Value: "1"}},
		},
		{
			name:      "no overlap appends overrides",
			base:      []corev1.EnvVar{{Name: "A", Value: "1"}},
			overrides: []corev1.EnvVar{{Name: "B", Value: "2"}},
			expected:  []corev1.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}},
		},
		{
			name:      "override replaces existing key in place",
			base:      []corev1.EnvVar{{Name: "A", Value: "old"}, {Name: "B", Value: "keep"}},
			overrides: []corev1.EnvVar{{Name: "A", Value: "new"}},
			expected:  []corev1.EnvVar{{Name: "A", Value: "new"}, {Name: "B", Value: "keep"}},
		},
		{
			name:      "mixed override and new keys",
			base:      []corev1.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}},
			overrides: []corev1.EnvVar{{Name: "B", Value: "override"}, {Name: "C", Value: "3"}},
			expected:  []corev1.EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "override"}, {Name: "C", Value: "3"}},
		},
		{
			name:      "base order is preserved and new overrides are appended",
			base:      []corev1.EnvVar{{Name: "Z", Value: "1"}, {Name: "A", Value: "2"}},
			overrides: []corev1.EnvVar{{Name: "M", Value: "3"}},
			expected:  []corev1.EnvVar{{Name: "Z", Value: "1"}, {Name: "A", Value: "2"}, {Name: "M", Value: "3"}},
		},
		{
			name: "dependent environment variables preserve expansion order",
			base: []corev1.EnvVar{
				{Name: "POSTGRES_HOST", Value: "postgres"},
				{Name: "POSTGRES_PORT", Value: "5432"},
				{Name: "POSTGRES_NO_DATABASE_URI", Value: "postgresql://$(POSTGRES_HOST):$(POSTGRES_PORT)"},
				{Name: "POSTGRES_DATABASE", Value: "reconciliation"},
				{Name: "POSTGRES_URI", Value: "$(POSTGRES_NO_DATABASE_URI)/$(POSTGRES_DATABASE)"},
			},
			overrides: []corev1.EnvVar{{Name: "PUBLISHER_NATS_ENABLED", Value: "true"}},
			expected: []corev1.EnvVar{
				{Name: "POSTGRES_HOST", Value: "postgres"},
				{Name: "POSTGRES_PORT", Value: "5432"},
				{Name: "POSTGRES_NO_DATABASE_URI", Value: "postgresql://$(POSTGRES_HOST):$(POSTGRES_PORT)"},
				{Name: "POSTGRES_DATABASE", Value: "reconciliation"},
				{Name: "POSTGRES_URI", Value: "$(POSTGRES_NO_DATABASE_URI)/$(POSTGRES_DATABASE)"},
				{Name: "PUBLISHER_NATS_ENABLED", Value: "true"},
			},
		},
		{
			name:      "both empty",
			base:      nil,
			overrides: nil,
			expected:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := MergeEnvVars(tc.base, tc.overrides)
			require.Equal(t, tc.expected, result)
		})
	}
}
