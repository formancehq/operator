package ledgers

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestLedgerV3EventSinksDisablesAllPreviewSinks(t *testing.T) {
	t.Parallel()

	configured := &ledgerv1alpha1.EventSinksSpec{NATS: []ledgerv1alpha1.NATSEventSinkSpec{{
		Name: "audit", URL: "nats://audit:4222", Topic: "audit",
	}}}
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}

	actual, err := ledgerV3EventSinks(newExportsContext(t), stack, configured, true)
	require.NoError(t, err)
	require.Empty(t, actual.NATS)
	require.Len(t, configured.NATS, 1)
}

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
