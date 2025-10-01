package brokers

import (
	"net/url"
	"testing"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mustParseURI(rawURL string) *v1beta1.URI {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return &v1beta1.URI{URL: u}
}

func TestGetPublisherEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		brokerMode       v1beta1.Mode
		brokerScheme     string
		stackName        string
		service          string
		prefix           string
		expectedMappings map[string]string // env var name -> expected value
	}{
		{
			name:         "OneStreamByService mode",
			brokerMode:   v1beta1.ModeOneStreamByService,
			brokerScheme: "nats",
			stackName:    "production",
			service:      "ledger",
			prefix:       "",
			expectedMappings: map[string]string{
				"PUBLISHER_TOPIC_MAPPING": "*:production-ledger",
			},
		},
		{
			name:         "OneStreamByService mode with custom prefix",
			brokerMode:   v1beta1.ModeOneStreamByService,
			brokerScheme: "kafka",
			stackName:    "staging",
			service:      "payments",
			prefix:       "CUSTOM_",
			expectedMappings: map[string]string{
				"CUSTOM_PUBLISHER_TOPIC_MAPPING": "*:staging-payments",
			},
		},
		{
			name:         "OneStreamByStack mode with NATS",
			brokerMode:   v1beta1.ModeOneStreamByStack,
			brokerScheme: "nats",
			stackName:    "production",
			service:      "ledger",
			prefix:       "",
			expectedMappings: map[string]string{
				"PUBLISHER_TOPIC_MAPPING":       "*:production.ledger",
				"PUBLISHER_NATS_AUTO_PROVISION": "false",
			},
		},
		{
			name:         "OneStreamByStack mode with Kafka",
			brokerMode:   v1beta1.ModeOneStreamByStack,
			brokerScheme: "kafka",
			stackName:    "staging",
			service:      "payments",
			prefix:       "",
			expectedMappings: map[string]string{
				"PUBLISHER_TOPIC_MAPPING": "*:staging.payments",
			},
		},
		{
			name:         "OneStreamByStack mode with NATS and custom prefix",
			brokerMode:   v1beta1.ModeOneStreamByStack,
			brokerScheme: "nats",
			stackName:    "dev",
			service:      "wallets",
			prefix:       "APP_",
			expectedMappings: map[string]string{
				"APP_PUBLISHER_TOPIC_MAPPING":       "*:dev.wallets",
				"APP_PUBLISHER_NATS_AUTO_PROVISION": "false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stack := &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.stackName,
				},
			}

			broker := &v1beta1.Broker{
				Status: v1beta1.BrokerStatus{
					Mode: tt.brokerMode,
					URI:  mustParseURI(tt.brokerScheme + "://test"),
				},
			}

			envVars := GetPublisherEnvVars(stack, broker, tt.service, tt.prefix)

			// Check that we got the expected number of env vars
			require.Len(t, envVars, len(tt.expectedMappings),
				"Expected %d env vars, got %d", len(tt.expectedMappings), len(envVars))

			// Check each expected env var
			for expectedName, expectedValue := range tt.expectedMappings {
				found := false
				for _, envVar := range envVars {
					if envVar.Name == expectedName {
						found = true
						require.Equal(t, expectedValue, envVar.Value,
							"Env var %s has wrong value", expectedName)
						break
					}
				}
				require.True(t, found, "Expected env var %s not found", expectedName)
			}
		})
	}
}

func TestGetPublisherEnvVarsModeFormatting(t *testing.T) {
	t.Parallel()

	// Test the specific formatting rules for each mode
	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-stack",
		},
	}

	t.Run("OneStreamByService uses hyphen separator", func(t *testing.T) {
		t.Parallel()

		broker := &v1beta1.Broker{
			Status: v1beta1.BrokerStatus{
				Mode: v1beta1.ModeOneStreamByService,
				URI:  mustParseURI("nats://test"),
			},
		}

		envVars := GetPublisherEnvVars(stack, broker, "my-service", "")

		var mappingValue string
		for _, env := range envVars {
			if env.Name == "PUBLISHER_TOPIC_MAPPING" {
				mappingValue = env.Value
				break
			}
		}

		require.Equal(t, "*:test-stack-my-service", mappingValue)
		require.Contains(t, mappingValue, "-", "Should use hyphen separator")
		require.NotContains(t, mappingValue, ".", "Should not use dot separator")
	})

	t.Run("OneStreamByStack uses dot separator", func(t *testing.T) {
		t.Parallel()

		broker := &v1beta1.Broker{
			Status: v1beta1.BrokerStatus{
				Mode: v1beta1.ModeOneStreamByStack,
				URI:  mustParseURI("kafka://test"),
			},
		}

		envVars := GetPublisherEnvVars(stack, broker, "my-service", "")

		var mappingValue string
		for _, env := range envVars {
			if env.Name == "PUBLISHER_TOPIC_MAPPING" {
				mappingValue = env.Value
				break
			}
		}

		require.Equal(t, "*:test-stack.my-service", mappingValue)
		require.Contains(t, mappingValue, ".", "Should use dot separator")
		require.NotContains(t, mappingValue, "-my-service", "Should not use hyphen before service")
	})
}

func TestGetPublisherEnvVarsNATSAutoProvision(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	t.Run("NATS OneStreamByStack sets auto_provision to false", func(t *testing.T) {
		t.Parallel()

		broker := &v1beta1.Broker{
			Status: v1beta1.BrokerStatus{
				Mode: v1beta1.ModeOneStreamByStack,
				URI:  mustParseURI("nats://test"),
			},
		}

		envVars := GetPublisherEnvVars(stack, broker, "service", "")

		var hasAutoProvision bool
		for _, env := range envVars {
			if env.Name == "PUBLISHER_NATS_AUTO_PROVISION" {
				hasAutoProvision = true
				require.Equal(t, "false", env.Value)
			}
		}

		require.True(t, hasAutoProvision, "Should have PUBLISHER_NATS_AUTO_PROVISION env var")
	})

	t.Run("Kafka OneStreamByStack does not set auto_provision", func(t *testing.T) {
		t.Parallel()

		broker := &v1beta1.Broker{
			Status: v1beta1.BrokerStatus{
				Mode: v1beta1.ModeOneStreamByStack,
				URI:  mustParseURI("kafka://test"),
			},
		}

		envVars := GetPublisherEnvVars(stack, broker, "service", "")

		for _, env := range envVars {
			require.NotEqual(t, "PUBLISHER_NATS_AUTO_PROVISION", env.Name,
				"Kafka should not have NATS auto provision env var")
		}
	})

	t.Run("NATS OneStreamByService does not set auto_provision", func(t *testing.T) {
		t.Parallel()

		broker := &v1beta1.Broker{
			Status: v1beta1.BrokerStatus{
				Mode: v1beta1.ModeOneStreamByService,
				URI:  mustParseURI("nats://test"),
			},
		}

		envVars := GetPublisherEnvVars(stack, broker, "service", "")

		for _, env := range envVars {
			require.NotEqual(t, "PUBLISHER_NATS_AUTO_PROVISION", env.Name,
				"OneStreamByService mode should not have NATS auto provision env var")
		}
	})
}