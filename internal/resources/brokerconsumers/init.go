package brokerconsumers

import (
	v1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
)

const deleteNatsConsumersFinalizer = "delete-nats-consumers"

func init() {
	core.Init(
		core.WithStackDependencyReconciler(Reconcile,
			core.WithFinalizer[*v1beta1.BrokerConsumer](deleteNatsConsumersFinalizer, deleteNatsConsumers),
			core.WithOwn[*v1beta1.BrokerConsumer](&v1beta1.BrokerTopic{}, builder.MatchEveryOwner),
			core.WithOwn[*v1beta1.BrokerConsumer](&v1.Job{}),
			brokers.Watch[*v1beta1.BrokerConsumer](),
		),
	)
}
