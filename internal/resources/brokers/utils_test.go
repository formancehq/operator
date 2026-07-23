package brokers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestGetBrokerEnvVarsCircuitBreakerIsOptIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		uri      string
		expected bool
	}{
		{
			name:     "disabled when setting is absent",
			uri:      "nats://nats:4222?replicas=3",
			expected: false,
		},
		{
			name:     "enabled by broker setting",
			uri:      "nats://nats:4222?circuitBreakerEnabled=true",
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			uri, err := v1beta1.ParseURL(test.uri)
			require.NoError(t, err)

			env, err := GetBrokerEnvVars(nil, uri, "stack", "service")
			require.NoError(t, err)

			enabled := false
			for _, variable := range env {
				if variable.Name == "PUBLISHER_CIRCUIT_BREAKER_ENABLED" {
					enabled = variable.Value == "true"
				}
			}
			require.Equal(t, test.expected, enabled)
		})
	}
}
