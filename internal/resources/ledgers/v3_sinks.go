package ledgers

import (
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
)

const ledgerV3BrokerSinkName = "formance-broker"

// ledgerV3EventSinks merges the sinks declared in LedgerConfiguration with the
// Formance broker sink. Returning a non-nil spec deliberately makes the
// configuration authoritative, so disabling the ledger BrokerTopic removes
// the previously managed sink as well.
func ledgerV3EventSinks(
	ctx core.Context,
	stack *v1beta1.Stack,
	configured *ledgerv1alpha1.EventSinksSpec,
	preview bool,
) (*ledgerv1alpha1.EventSinksSpec, error) {
	// Preview clusters must not duplicate events emitted by the active Ledger.
	if preview {
		return &ledgerv1alpha1.EventSinksSpec{}, nil
	}

	topic := &v1beta1.BrokerTopic{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Name: core.GetObjectName(stack.Name, "ledger"),
	}, topic); err != nil {
		if apierrors.IsNotFound(err) {
			return mergeLedgerV3EventSinks(configured, nil), nil
		}
		return nil, fmt.Errorf("getting Ledger BrokerTopic: %w", err)
	}
	if !topic.Status.Ready {
		return nil, core.NewPendingError().WithMessage("Ledger BrokerTopic is not ready")
	}

	broker := &v1beta1.Broker{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: stack.Name}, broker); err != nil {
		return nil, fmt.Errorf("getting Broker for Ledger v3 sink: %w", err)
	}
	if !broker.Status.Ready || broker.Status.URI == nil {
		return nil, core.NewPendingError().WithMessage("broker is not ready for Ledger v3 sink")
	}
	if broker.Status.URI.Scheme != "nats" {
		return mergeLedgerV3EventSinks(configured, nil), nil
	}

	sink := &ledgerv1alpha1.NATSEventSinkSpec{
		Name:   ledgerV3BrokerSinkName,
		URL:    fmt.Sprintf("nats://%s", broker.Status.URI.Host),
		Topic:  brokers.GetPublisherTopic(stack, broker, "ledger"),
		Format: "json",
	}
	return mergeLedgerV3EventSinks(configured, sink), nil
}

func mergeLedgerV3EventSinks(
	configured *ledgerv1alpha1.EventSinksSpec,
	brokerSink *ledgerv1alpha1.NATSEventSinkSpec,
) *ledgerv1alpha1.EventSinksSpec {
	sinks := &ledgerv1alpha1.EventSinksSpec{}
	if configured != nil {
		sinks = configured.DeepCopy()
	}
	sinks.NATS = slices.DeleteFunc(sinks.NATS, func(sink ledgerv1alpha1.NATSEventSinkSpec) bool {
		return sink.Name == ledgerV3BrokerSinkName
	})
	if brokerSink != nil {
		sinks.NATS = append(sinks.NATS, *brokerSink)
	}
	return sinks
}
