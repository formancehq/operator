package brokers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestBrokerNeedsStreamSubjectsMigration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  v1beta1.BrokerStatus
		service string
		want    bool
	}{
		{
			name:    "migrates every existing stream when the revision is absent",
			status:  v1beta1.BrokerStatus{Streams: []string{"ledger"}},
			service: "ledger",
			want:    true,
		},
		{
			name: "does not recreate a completed migration job",
			status: v1beta1.BrokerStatus{
				Streams:                []string{"ledger"},
				StreamSubjectsRevision: natsNestedSubjectsRevision,
			},
			service: "ledger",
			want:    false,
		},
		{
			name: "creates the stream for a service added after migration",
			status: v1beta1.BrokerStatus{
				Streams:                []string{"payments"},
				StreamSubjectsRevision: natsNestedSubjectsRevision,
			},
			service: "ledger",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broker := &v1beta1.Broker{Status: tt.status}
			require.Equal(t, tt.want, brokerNeedsStreamSubjectsMigration(broker, tt.service))
		})
	}
}
