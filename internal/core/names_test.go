package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestGetObjectName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		stack    string
		objName  string
		expected string
	}{
		{
			name:     "simple names",
			stack:    "production",
			objName:  "ledger",
			expected: "production-ledger",
		},
		{
			name:     "with hyphens",
			stack:    "staging-eu",
			objName:  "payments-service",
			expected: "staging-eu-payments-service",
		},
		{
			name:     "short names",
			stack:    "dev",
			objName:  "db",
			expected: "dev-db",
		},
		{
			name:     "empty stack",
			stack:    "",
			objName:  "ledger",
			expected: "-ledger",
		},
		{
			name:     "empty name",
			stack:    "production",
			objName:  "",
			expected: "production-",
		},
		{
			name:     "both empty",
			stack:    "",
			objName:  "",
			expected: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GetObjectName(tt.stack, tt.objName)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetNamespacedResourceName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		namespace string
		resName   string
		expected  types.NamespacedName
	}{
		{
			name:      "typical resource",
			namespace: "production",
			resName:   "ledger-deployment",
			expected: types.NamespacedName{
				Namespace: "production",
				Name:      "ledger-deployment",
			},
		},
		{
			name:      "default namespace",
			namespace: "default",
			resName:   "service",
			expected: types.NamespacedName{
				Namespace: "default",
				Name:      "service",
			},
		},
		{
			name:      "empty namespace",
			namespace: "",
			resName:   "resource",
			expected: types.NamespacedName{
				Namespace: "",
				Name:      "resource",
			},
		},
		{
			name:      "empty name",
			namespace: "production",
			resName:   "",
			expected: types.NamespacedName{
				Namespace: "production",
				Name:      "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GetNamespacedResourceName(tt.namespace, tt.resName)
			require.Equal(t, tt.expected, result)
			require.Equal(t, tt.expected.Namespace, result.Namespace)
			require.Equal(t, tt.expected.Name, result.Name)
		})
	}
}

func TestGetResourceName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		resName  string
		expected types.NamespacedName
	}{
		{
			name:    "typical resource",
			resName: "my-resource",
			expected: types.NamespacedName{
				Namespace: "",
				Name:      "my-resource",
			},
		},
		{
			name:    "cluster-scoped resource",
			resName: "clusterrole-admin",
			expected: types.NamespacedName{
				Namespace: "",
				Name:      "clusterrole-admin",
			},
		},
		{
			name:    "empty name",
			resName: "",
			expected: types.NamespacedName{
				Namespace: "",
				Name:      "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GetResourceName(tt.resName)
			require.Equal(t, tt.expected, result)
			require.Empty(t, result.Namespace, "Namespace should always be empty")
			require.Equal(t, tt.expected.Name, result.Name)
		})
	}
}