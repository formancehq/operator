package brokertopics

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func Create(ctx core.Context, stack *v1beta1.Stack, owner interface {
	client.Object
	GetStack() string
}, service string) (*v1beta1.BrokerTopic, error) {
	topic, _, err := core.CreateOrUpdate[*v1beta1.BrokerTopic](ctx, types.NamespacedName{
		Name: core.GetObjectName(stack.Name, service),
	}, func(t *v1beta1.BrokerTopic) error {
		t.Spec.Stack = owner.GetStack()
		t.Spec.Service = service

		if err := controllerutil.SetOwnerReference(owner, t, ctx.GetScheme()); err != nil {
			return err
		}

		if err := controllerutil.SetOwnerReference(stack, t, ctx.GetScheme()); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return topic, nil
}

func Find(ctx core.Context, stack *v1beta1.Stack, name string) (*v1beta1.BrokerTopic, error) {
	topicList := &v1beta1.BrokerTopicList{}
	if err := ctx.GetClient().List(ctx, topicList, client.MatchingFields{
		".spec.service": name,
		"stack":         stack.Name,
	}); err != nil {
		return nil, err
	}

	if len(topicList.Items) == 0 {
		return nil, nil
	}

	return &topicList.Items[0], nil
}
