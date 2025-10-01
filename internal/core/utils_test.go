package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestHashFromConfigMaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configMaps  []*corev1.ConfigMap
		expectSame  bool
		compareWith []*corev1.ConfigMap
	}{
		{
			name: "single configmap",
			configMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cm",
					},
					Data: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			expectSame: true,
			compareWith: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cm",
					},
					Data: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
		},
		{
			name: "different data should produce different hash",
			configMaps: []*corev1.ConfigMap{
				{
					Data: map[string]string{
						"key1": "value1",
					},
				},
			},
			expectSame: false,
			compareWith: []*corev1.ConfigMap{
				{
					Data: map[string]string{
						"key1": "value2",
					},
				},
			},
		},
		{
			name: "multiple configmaps",
			configMaps: []*corev1.ConfigMap{
				{
					Data: map[string]string{
						"key1": "value1",
					},
				},
				{
					Data: map[string]string{
						"key2": "value2",
					},
				},
			},
			expectSame: true,
			compareWith: []*corev1.ConfigMap{
				{
					Data: map[string]string{
						"key1": "value1",
					},
				},
				{
					Data: map[string]string{
						"key2": "value2",
					},
				},
			},
		},
		{
			name: "order matters for multiple configmaps",
			configMaps: []*corev1.ConfigMap{
				{
					Data: map[string]string{
						"key1": "value1",
					},
				},
				{
					Data: map[string]string{
						"key2": "value2",
					},
				},
			},
			expectSame: false,
			compareWith: []*corev1.ConfigMap{
				{
					Data: map[string]string{
						"key2": "value2",
					},
				},
				{
					Data: map[string]string{
						"key1": "value1",
					},
				},
			},
		},
		{
			name: "empty configmap",
			configMaps: []*corev1.ConfigMap{
				{
					Data: map[string]string{},
				},
			},
			expectSame: true,
			compareWith: []*corev1.ConfigMap{
				{
					Data: map[string]string{},
				},
			},
		},
		{
			name: "metadata changes don't affect hash (only data matters)",
			configMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cm1",
						Labels: map[string]string{
							"label": "value",
						},
					},
					Data: map[string]string{
						"key": "value",
					},
				},
			},
			expectSame: true,
			compareWith: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cm2",
						Labels: map[string]string{
							"different": "labels",
						},
					},
					Data: map[string]string{
						"key": "value",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hash1 := HashFromConfigMaps(tt.configMaps...)
			hash2 := HashFromConfigMaps(tt.compareWith...)

			// Verify hash is not empty
			require.NotEmpty(t, hash1)
			require.NotEmpty(t, hash2)

			if tt.expectSame {
				require.Equal(t, hash1, hash2, "Hashes should be equal")
			} else {
				require.NotEqual(t, hash1, hash2, "Hashes should be different")
			}
		})
	}
}

func TestHashFromResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		resources   []*unstructured.Unstructured
		expectSame  bool
		compareWith []*unstructured.Unstructured
	}{
		{
			name: "single resource",
			resources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]interface{}{
							"name":            "test",
							"uid":             "123",
							"resourceVersion": "1",
						},
					},
				},
			},
			expectSame: true,
			compareWith: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]interface{}{
							"name":            "test",
							"uid":             "123",
							"resourceVersion": "1",
						},
					},
				},
			},
		},
		{
			name: "different UID produces different hash",
			resources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "123",
							"resourceVersion": "1",
						},
					},
				},
			},
			expectSame: false,
			compareWith: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "456",
							"resourceVersion": "1",
						},
					},
				},
			},
		},
		{
			name: "different resourceVersion produces different hash",
			resources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "123",
							"resourceVersion": "1",
						},
					},
				},
			},
			expectSame: false,
			compareWith: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "123",
							"resourceVersion": "2",
						},
					},
				},
			},
		},
		{
			name: "multiple resources",
			resources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "123",
							"resourceVersion": "1",
						},
					},
				},
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "456",
							"resourceVersion": "2",
						},
					},
				},
			},
			expectSame: true,
			compareWith: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "123",
							"resourceVersion": "1",
						},
					},
				},
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "456",
							"resourceVersion": "2",
						},
					},
				},
			},
		},
		{
			name: "order matters",
			resources: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "123",
							"resourceVersion": "1",
						},
					},
				},
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "456",
							"resourceVersion": "2",
						},
					},
				},
			},
			expectSame: false,
			compareWith: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "456",
							"resourceVersion": "2",
						},
					},
				},
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"uid":             "123",
							"resourceVersion": "1",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hash1 := HashFromResources(tt.resources...)
			hash2 := HashFromResources(tt.compareWith...)

			// Verify hash is not empty
			require.NotEmpty(t, hash1)
			require.NotEmpty(t, hash2)

			if tt.expectSame {
				require.Equal(t, hash1, hash2, "Hashes should be equal")
			} else {
				require.NotEqual(t, hash1, hash2, "Hashes should be different")
			}
		})
	}
}

func TestHashFromConfigMapsIsConsistent(t *testing.T) {
	t.Parallel()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	// Hash should be deterministic
	hash1 := HashFromConfigMaps(cm)
	hash2 := HashFromConfigMaps(cm)
	hash3 := HashFromConfigMaps(cm)

	require.Equal(t, hash1, hash2)
	require.Equal(t, hash2, hash3)
}

func TestHashFromResourcesIsConsistent(t *testing.T) {
	t.Parallel()

	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"uid":             "123",
				"resourceVersion": "1",
			},
		},
	}

	// Hash should be deterministic
	hash1 := HashFromResources(resource)
	hash2 := HashFromResources(resource)
	hash3 := HashFromResources(resource)

	require.Equal(t, hash1, hash2)
	require.Equal(t, hash2, hash3)
}

func TestShellScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cmd         string
		args        []any
		shouldMatch string
	}{
		{
			name:        "simple command without args",
			cmd:         "echo hello",
			args:        nil,
			shouldMatch: "echo hello",
		},
		{
			name:        "command with string formatting",
			cmd:         "echo %s",
			args:        []any{"world"},
			shouldMatch: "echo world",
		},
		{
			name:        "command with multiple args",
			cmd:         "echo %s %s",
			args:        []any{"hello", "world"},
			shouldMatch: "echo hello world",
		},
		{
			name:        "command with integer",
			cmd:         "sleep %d",
			args:        []any{5},
			shouldMatch: "sleep 5",
		},
		{
			name:        "complex command with pipes",
			cmd:         "cat %s | grep %s",
			args:        []any{"/tmp/file.txt", "error"},
			shouldMatch: "cat /tmp/file.txt | grep error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ShellScript(tt.cmd, tt.args...)

			require.Len(t, result, 3, "ShellScript should always return 3 elements")
			require.Equal(t, "sh", result[0], "First element should be sh")
			require.Equal(t, "-c", result[1], "Second element should be -c")

			// The third element is a heredoc script that should contain our command
			require.Contains(t, result[2], tt.shouldMatch, "Script should contain the formatted command")
			require.Contains(t, result[2], "/bin/sh <<'EOF'", "Script should use heredoc")
			require.Contains(t, result[2], "set -x", "Script should enable trace mode")
		})
	}
}

func TestShellScriptFormat(t *testing.T) {
	t.Parallel()

	// Test that the script is properly formatted
	result := ShellScript("echo %s %d %v", "test", 42, true)

	require.Len(t, result, 3)
	require.Equal(t, "sh", result[0])
	require.Equal(t, "-c", result[1])

	// Check the command is properly formatted within the heredoc
	require.Contains(t, result[2], "echo test 42 true")
}