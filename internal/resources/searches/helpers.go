package searches

import (
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/brokerconsumers"
)

func createConsumers(ctx Context, search *v1beta1.Search) error {
	for _, o := range []v1beta1.Module{
		&v1beta1.Payments{},
		&v1beta1.Ledger{},
		&v1beta1.Gateway{},
	} {
		if ok, err := HasDependency(ctx, search.Spec.Stack, o); err != nil {
			return err
		} else if ok {
			consumer, err := brokerconsumers.Create(ctx, search, LowerCamelCaseKind(ctx, o), LowerCamelCaseKind(ctx, o))
			if err != nil {
				return err
			}
			if !consumer.Status.Ready {
				return NewPendingError().WithMessage("waiting for consumer %s to be ready", consumer.Name)
			}
		}
	}

	return nil
}
