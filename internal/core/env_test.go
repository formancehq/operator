package core

import (
	"testing"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnv(t *testing.T) {
	t.Parallel()
	envVar := Env("TEST_KEY", "test-value")

	require.Equal(t, "TEST_KEY", envVar.Name)
	require.Equal(t, "test-value", envVar.Value)
	require.Nil(t, envVar.ValueFrom)
}

func TestEnvFromBool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    bool
		expected string
	}{
		{
			name:     "true value",
			input:    true,
			expected: "true",
		},
		{
			name:     "false value",
			input:    false,
			expected: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			envVar := EnvFromBool("DEBUG", tt.input)

			require.Equal(t, "DEBUG", envVar.Name)
			require.Equal(t, tt.expected, envVar.Value)
			require.Nil(t, envVar.ValueFrom)
		})
	}
}

func TestEnvFromConfig(t *testing.T) {
	t.Parallel()
	envVar := EnvFromConfig("DATABASE_URL", "db-config", "url")

	require.Equal(t, "DATABASE_URL", envVar.Name)
	require.Empty(t, envVar.Value)
	require.NotNil(t, envVar.ValueFrom)
	require.NotNil(t, envVar.ValueFrom.ConfigMapKeyRef)
	require.Equal(t, "db-config", envVar.ValueFrom.ConfigMapKeyRef.Name)
	require.Equal(t, "url", envVar.ValueFrom.ConfigMapKeyRef.Key)
}

func TestEnvFromSecret(t *testing.T) {
	t.Parallel()
	envVar := EnvFromSecret("API_KEY", "api-secrets", "key")

	require.Equal(t, "API_KEY", envVar.Name)
	require.Empty(t, envVar.Value)
	require.NotNil(t, envVar.ValueFrom)
	require.NotNil(t, envVar.ValueFrom.SecretKeyRef)
	require.Equal(t, "api-secrets", envVar.ValueFrom.SecretKeyRef.Name)
	require.Equal(t, "key", envVar.ValueFrom.SecretKeyRef.Key)
}

func TestEnvVarPlaceholder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "simple key",
			key:      "DATABASE_URL",
			expected: "$(DATABASE_URL)",
		},
		{
			name:     "key with underscores",
			key:      "OTEL_EXPORTER_ENDPOINT",
			expected: "$(OTEL_EXPORTER_ENDPOINT)",
		},
		{
			name:     "empty key",
			key:      "",
			expected: "$()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := EnvVarPlaceholder(tt.key)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestComputeEnvVar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		format   string
		keys     []string
		expected string
	}{
		{
			name:     "single placeholder",
			format:   "http://%s",
			keys:     []string{"HOST"},
			expected: "http://$(HOST)",
		},
		{
			name:     "multiple placeholders",
			format:   "http://%s:%s",
			keys:     []string{"HOST", "PORT"},
			expected: "http://$(HOST):$(PORT)",
		},
		{
			name:     "complex format",
			format:   "%s://%s/api/%s",
			keys:     []string{"SCHEME", "HOST", "VERSION"},
			expected: "$(SCHEME)://$(HOST)/api/$(VERSION)",
		},
		{
			name:     "no placeholders",
			format:   "static-value",
			keys:     []string{},
			expected: "static-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ComputeEnvVar(tt.format, tt.keys...)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDevEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stackDebug  bool
		stackDev    bool
		serviceDebug bool
		serviceDev   bool
		expected    map[string]string
	}{
		{
			name:         "all false",
			stackDebug:   false,
			stackDev:     false,
			serviceDebug: false,
			serviceDev:   false,
			expected: map[string]string{
				"DEBUG": "false",
				"DEV":   "false",
				"STACK": "test-stack",
			},
		},
		{
			name:         "stack debug enabled",
			stackDebug:   true,
			stackDev:     false,
			serviceDebug: false,
			serviceDev:   false,
			expected: map[string]string{
				"DEBUG": "true",
				"DEV":   "false",
				"STACK": "test-stack",
			},
		},
		{
			name:         "service debug enabled",
			stackDebug:   false,
			stackDev:     false,
			serviceDebug: true,
			serviceDev:   false,
			expected: map[string]string{
				"DEBUG": "true",
				"DEV":   "false",
				"STACK": "test-stack",
			},
		},
		{
			name:         "both debug enabled",
			stackDebug:   true,
			stackDev:     false,
			serviceDebug: true,
			serviceDev:   false,
			expected: map[string]string{
				"DEBUG": "true",
				"DEV":   "false",
				"STACK": "test-stack",
			},
		},
		{
			name:         "stack dev enabled",
			stackDebug:   false,
			stackDev:     true,
			serviceDebug: false,
			serviceDev:   false,
			expected: map[string]string{
				"DEBUG": "false",
				"DEV":   "true",
				"STACK": "test-stack",
			},
		},
		{
			name:         "all enabled",
			stackDebug:   true,
			stackDev:     true,
			serviceDebug: true,
			serviceDev:   true,
			expected: map[string]string{
				"DEBUG": "true",
				"DEV":   "true",
				"STACK": "test-stack",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stack := &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-stack",
				},
				Spec: v1beta1.StackSpec{
					DevProperties: v1beta1.DevProperties{
						Debug: tt.stackDebug,
						Dev:   tt.stackDev,
					},
				},
			}

			service := &v1beta1.DevProperties{
				Debug: tt.serviceDebug,
				Dev:   tt.serviceDev,
			}

			envVars := GetDevEnvVars(stack, service)

			require.Len(t, envVars, 3)

			for _, envVar := range envVars {
				expectedValue, ok := tt.expected[envVar.Name]
				require.True(t, ok, "Unexpected env var: %s", envVar.Name)
				require.Equal(t, expectedValue, envVar.Value,
					"Wrong value for %s: got %s, expected %s",
					envVar.Name, envVar.Value, expectedValue)
			}
		})
	}
}

func TestGetDevEnvVarsWithPrefix(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-stack",
		},
		Spec: v1beta1.StackSpec{
			DevProperties: v1beta1.DevProperties{
				Debug: true,
				Dev:   false,
			},
		},
	}

	service := &v1beta1.DevProperties{
		Debug: false,
		Dev:   true,
	}

	envVars := GetDevEnvVarsWithPrefix(stack, service, "CUSTOM_")

	require.Len(t, envVars, 3)

	expected := map[string]string{
		"CUSTOM_DEBUG": "true",
		"CUSTOM_DEV":   "true",
		"CUSTOM_STACK": "test-stack",
	}

	for _, envVar := range envVars {
		expectedValue, ok := expected[envVar.Name]
		require.True(t, ok, "Unexpected env var: %s", envVar.Name)
		require.Equal(t, expectedValue, envVar.Value)
	}
}