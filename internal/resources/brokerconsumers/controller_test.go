package brokerconsumers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestNatsConsumerSubjects(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"

	tests := []struct {
		name     string
		mode     v1beta1.Mode
		expected []string
	}{
		{
			name: "one stream by stack",
			mode: v1beta1.ModeOneStreamByStack,
			expected: []string{
				"stack0.ledger", "stack0.ledger.>",
				"stack0.payments", "stack0.payments.>",
			},
		},
		{
			name: "one stream by service",
			mode: v1beta1.ModeOneStreamByService,
			expected: []string{
				"stack0-ledger", "stack0-ledger.>",
				"stack0-payments", "stack0-payments.>",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			broker := &v1beta1.Broker{Status: v1beta1.BrokerStatus{Mode: test.mode}}
			require.Equal(t, test.expected, natsConsumerSubjects(stack, broker, "ledger", "payments"))
		})
	}
}

func TestNatsServiceConditionReasonIncludesRevision(t *testing.T) {
	t.Parallel()

	require.Equal(t, "ledger:NestedSubjectsV1", natsServiceConditionReason("ledger"))
}
