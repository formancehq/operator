package brokers

import (
	v1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

//+kubebuilder:rbac:groups=formance.com,resources=brokers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=brokers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=brokers/finalizers,verbs=update

func init() {
	core.Init(
		core.WithResourceReconciler(Reconcile,
			core.WithFinalizer[*v1beta1.Broker]("clear", deleteBroker),
			core.WithOwn[*v1beta1.Broker](&v1.Job{}),
			core.WithWatchSettings[*v1beta1.Broker](),
			// In one-stream-by-service mode, new topics require the broker to reconcile
			// and provision the matching stream.
			core.WithWatch[*v1beta1.Broker, *v1beta1.BrokerTopic](func(ctx core.Context, topic *v1beta1.BrokerTopic) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Name: topic.Spec.Stack,
					},
				}}
			}),
		),
	)
}
