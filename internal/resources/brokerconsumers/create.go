package brokerconsumers

import (
	"fmt"
	"sort"

	"github.com/iancoleman/strcase"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/formancehq/go-libs/v2/collectionutils"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func Create(ctx core.Context, owner interface {
	client.Object
	GetStack() string
}, name string, services ...string) (*v1beta1.BrokerConsumer, error) {
	kind := owner.GetObjectKind().GroupVersionKind().Kind
	queriedBy := strcase.ToKebab(kind)

	sort.Strings(services)

	brokerConsumerName := fmt.Sprintf("%s-%s", owner.GetName(), strcase.ToKebab(kind))
	if name != "" {
		brokerConsumerName += "-" + name
	}

	brokerConsumer, _, err := core.CreateOrUpdate[*v1beta1.BrokerConsumer](ctx, types.NamespacedName{
		Name: brokerConsumerName,
	},
		func(t *v1beta1.BrokerConsumer) error {
			t.Spec.QueriedBy = queriedBy
			t.Spec.Stack = owner.GetStack()
			t.Spec.Services = services
			t.Spec.Name = name

			return nil
		},
		core.WithController[*v1beta1.BrokerConsumer](ctx.GetScheme(), owner),
	)
	if err != nil {
		return nil, err
	}

	return brokerConsumer, nil
}

func CreateOrUpdateOnAllServices(ctx core.Context, consumer interface {
	client.Object
	GetStack() string
}, includeItself bool) (*v1beta1.BrokerConsumer, error) {
	services, err := core.ListEventPublishers(ctx, consumer.GetStack())
	if err != nil {
		return nil, err
	}

	filteredServices := Filter(services, func(u unstructured.Unstructured) bool {
		if !includeItself {
			return u.GetKind() != consumer.GetObjectKind().GroupVersionKind().Kind
		}
		return true
	})

	return Create(ctx, consumer, "", Map(filteredServices, func(from unstructured.Unstructured) string {
		return strcase.ToKebab(from.GetKind())
	})...)
}
