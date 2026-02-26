package transactionplane

import (
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/brokerconsumers"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
	"github.com/formancehq/operator/v3/internal/resources/brokertopics"
	"github.com/formancehq/operator/v3/internal/resources/databases"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/registries"
)

//+kubebuilder:rbac:groups=formance.com,resources=transactionplanes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=transactionplanes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=transactionplanes/finalizers,verbs=update

func Reconcile(ctx Context, stack *v1beta1.Stack, t *v1beta1.TransactionPlane, version string) error {

	database, err := databases.Create(ctx, stack, t)
	if err != nil {
		return err
	}

	authClient, err := createAuthClient(ctx, stack, t)
	if err != nil {
		return err
	}

	consumer, err := brokerconsumers.CreateOrUpdateOnAllServices(ctx, t, true)
	if err != nil {
		return err
	}

	if err := gatewayhttpapis.Create(ctx, t, gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck")); err != nil {
		return err
	}

	if !database.Status.Ready {
		return NewPendingError().WithMessage("database not ready")
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "transaction-plane", version)
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
		if err := createDeployments(ctx, stack, t, database, authClient, consumer, imageConfiguration); err != nil {
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
			WithOwn[*v1beta1.TransactionPlane](&v1beta1.BrokerConsumer{}),
			WithOwn[*v1beta1.TransactionPlane](&v1beta1.AuthClient{}),
			WithOwn[*v1beta1.TransactionPlane](&appsv1.Deployment{}),
			WithOwn[*v1beta1.TransactionPlane](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.TransactionPlane](&batchv1.Job{}),
			WithOwn[*v1beta1.TransactionPlane](&v1beta1.ResourceReference{}),
			WithWatchSettings[*v1beta1.TransactionPlane](),
			WithWatchDependency[*v1beta1.TransactionPlane](&v1beta1.Ledger{}),
			WithWatchDependency[*v1beta1.TransactionPlane](&v1beta1.Auth{}),
			WithWatchDependency[*v1beta1.TransactionPlane](&v1beta1.Payments{}),
			brokertopics.Watch[*v1beta1.TransactionPlane]("transaction-plane"),
			databases.Watch[*v1beta1.TransactionPlane](),
			brokers.Watch[*v1beta1.TransactionPlane](),
		),
	)
}
