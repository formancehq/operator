/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package orchestrations

import (
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/brokerconsumers"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
)

//+kubebuilder:rbac:groups=formance.com,resources=orchestrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=orchestrations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=orchestrations/finalizers,verbs=update

func Reconcile(ctx Context, stack *v1beta1.Stack, o *v1beta1.Orchestration, version string) error {

	database, err := databases.Create(ctx, stack, o)
	if err != nil {
		return err
	}

	authClient, err := createAuthClient(ctx, stack, o)
	if err != nil {
		return err
	}

	consumer, err := brokerconsumers.CreateOrUpdateOnAllServices(ctx, o)
	if err != nil {
		return err
	}

	if err := gatewayhttpapis.Create(ctx, o, gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck")); err != nil {
		return err
	}

	if !database.Status.Ready {
		return NewPendingError().WithMessage("database not ready")
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "orchestration", version)
	if err != nil {
		return errors.Wrap(err, "resolving image")
	}

	if IsGreaterOrEqual(version, "v2.0.0-rc.5") && databases.GetSavedModuleVersion(database) != version {

		if err := databases.Migrate(ctx, stack, o, imageConfiguration, database); err != nil {
			return err
		}

		if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
			return errors.Wrap(err, "saving module version in database object")
		}
	}

	if consumer.Status.Ready {
		if err := createDeployment(ctx, stack, o, database, authClient, consumer, imageConfiguration); err != nil {
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
			WithOwn[*v1beta1.Orchestration](&v1beta1.BrokerConsumer{}),
			WithOwn[*v1beta1.Orchestration](&v1beta1.AuthClient{}),
			WithOwn[*v1beta1.Orchestration](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Orchestration](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Orchestration](&v1beta1.ResourceReference{}),
			WithOwn[*v1beta1.Orchestration](&batchv1.Job{}),
			WithWatchSettings[*v1beta1.Orchestration](),
			WithWatchDependency[*v1beta1.Orchestration](&v1beta1.Ledger{}),
			WithWatchDependency[*v1beta1.Orchestration](&v1beta1.Auth{}),
			WithWatchDependency[*v1beta1.Orchestration](&v1beta1.Payments{}),
			WithWatchDependency[*v1beta1.Orchestration](&v1beta1.Wallets{}),
			brokertopics.Watch[*v1beta1.Orchestration]("orchestration"),
			databases.Watch[*v1beta1.Orchestration](),
		),
	)
}
