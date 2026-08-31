package ledgers

import (
	"testing"

	"github.com/stretchr/testify/require"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"
)

func TestMergeLedgerV3EventSinks(t *testing.T) {
	t.Parallel()

	configured := &ledgerv1alpha1.EventSinksSpec{NATS: []ledgerv1alpha1.NATSEventSinkSpec{
		{Name: "audit", URL: "nats://audit:4222", Topic: "audit"},
		{Name: ledgerV3BrokerSinkName, URL: "nats://stale:4222", Topic: "stale"},
	}}
	brokerSink := &ledgerv1alpha1.NATSEventSinkSpec{
		Name: ledgerV3BrokerSinkName, URL: "nats://broker:4222", Topic: "stack0.ledger", Format: "json",
	}

	actual := mergeLedgerV3EventSinks(configured, brokerSink)
	require.Equal(t, []ledgerv1alpha1.NATSEventSinkSpec{
		{Name: "audit", URL: "nats://audit:4222", Topic: "audit"},
		*brokerSink,
	}, actual.NATS)
	require.Equal(t, "stale", configured.NATS[1].Topic)
}

func TestMergeLedgerV3EventSinksRemovesManagedSinkWhenDisabled(t *testing.T) {
	t.Parallel()

	configured := &ledgerv1alpha1.EventSinksSpec{NATS: []ledgerv1alpha1.NATSEventSinkSpec{
		{Name: "audit", URL: "nats://audit:4222", Topic: "audit"},
		{Name: ledgerV3BrokerSinkName, URL: "nats://stale:4222", Topic: "stale"},
	}}

	actual := mergeLedgerV3EventSinks(configured, nil)
	require.Equal(t, []ledgerv1alpha1.NATSEventSinkSpec{
		{Name: "audit", URL: "nats://audit:4222", Topic: "audit"},
	}, actual.NATS)
}
