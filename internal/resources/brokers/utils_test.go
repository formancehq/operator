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

func TestGetPublisherTopic(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"

	tests := []struct {
		name     string
		mode     v1beta1.Mode
		expected string
	}{
		{name: "one stream by service", mode: v1beta1.ModeOneStreamByService, expected: "stack0-ledger"},
		{name: "one stream by stack", mode: v1beta1.ModeOneStreamByStack, expected: "stack0.ledger"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			broker := &v1beta1.Broker{Status: v1beta1.BrokerStatus{Mode: test.mode}}
			require.Equal(t, test.expected, GetPublisherTopic(stack, broker, "ledger"))
		})
	}
}

func TestNatsSubjects(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"stack0.ledger", "stack0.ledger.>"}, NatsSubjects("stack0.ledger"))
}
