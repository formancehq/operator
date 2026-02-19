package transactions

import (
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/brokerconsumers"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/registries"
)

//+kubebuilder:rbac:groups=formance.com,resources=transactions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=transactions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=transactions/finalizers,verbs=update

func Reconcile(ctx Context, stack *v1beta1.Stack, t *v1beta1.Transactions, version string) error {

	database, err := databases.Create(ctx, stack, t)
	if err != nil {
		return err
	}

	authClient, err := createAuthClient(ctx, stack, t)
	if err != nil {
		return err
	}

	consumer, err := brokerconsumers.CreateOrUpdateOnAllServices(ctx, t)
	if err != nil {
		return err
	}

	if err := gatewayhttpapis.Create(ctx, t, gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck")); err != nil {
		return err
	}

	if !database.Status.Ready {
		return NewPendingError().WithMessage("database not ready")
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "transactions", version)
	if err != nil {
		return errors.Wrap(err, "resolving image")
	}

	if databases.GetSavedModuleVersion(database) != version {
		if err := databases.Migrate(ctx, stack, t, imageConfiguration, database); err != nil {
			return err
		}

		if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
			return errors.Wrap(err, "saving module version in database object")
		}
	}

	if consumer.Status.Ready {
		if err := createDeployment(ctx, stack, t, database, authClient, consumer, imageConfiguration); err != nil {
			return err
		}
	} else {
		return NewPendingError().WithMessage("waiting for consumers to be ready")
	}

	return nil
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			WithOwn[*v1beta1.Transactions](&v1beta1.BrokerConsumer{}),
			WithOwn[*v1beta1.Transactions](&v1beta1.AuthClient{}),
			WithOwn[*v1beta1.Transactions](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Transactions](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Transactions](&batchv1.Job{}),
			WithWatchSettings[*v1beta1.Transactions](),
			WithWatchDependency[*v1beta1.Transactions](&v1beta1.Ledger{}),
			WithWatchDependency[*v1beta1.Transactions](&v1beta1.Auth{}),
			WithWatchDependency[*v1beta1.Transactions](&v1beta1.Payments{}),
			brokertopics.Watch[*v1beta1.Transactions]("transactions"),
			databases.Watch[*v1beta1.Transactions](),
			brokers.Watch[*v1beta1.Transactions](),
		),
	)
}
