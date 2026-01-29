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

package reconciliations

import (
	"fmt"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	v1beta1 "github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/authclients"
	"github.com/formancehq/operator/internal/resources/brokerconsumers"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/registries"
)

//+kubebuilder:rbac:groups=formance.com,resources=reconciliations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=reconciliations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=reconciliations/finalizers,verbs=update

func Reconcile(ctx Context, stack *v1beta1.Stack, reconciliation *v1beta1.Reconciliation, version string) error {
	// Clean up legacy single worker consumer and deployment
	if err := deleteLegacyResources(ctx, reconciliation); err != nil {
		return err
	}

	database, err := databases.Create(ctx, stack, reconciliation)
	if err != nil {
		return err
	}

	ingestionConsumer, err := brokerconsumers.Create(ctx, reconciliation, "ingestion", "ledger", "payments")
	if err != nil {
		return err
	}

	matchingConsumer, err := brokerconsumers.Create(ctx, reconciliation, "matching", "reconciliation")
	if err != nil {
		return err
	}

	authClient, err := authclients.Create(ctx, stack, reconciliation, "reconciliation",
		authclients.WithScopes("ledger:read", "payments:read"))
	if err != nil {
		return err
	}

	if err := gatewayhttpapis.Create(ctx, reconciliation, gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck")); err != nil {
		return err
	}

	if !database.Status.Ready {
		return NewPendingError().WithMessage("database not ready")
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "reconciliation", version)
	if err != nil {
		return errors.Wrap(err, "resolving image")
	}

	if IsGreaterOrEqual(version, "v2.0.0-rc.5") && databases.GetSavedModuleVersion(database) != version {

		if err := databases.Migrate(ctx, stack, reconciliation, imageConfiguration, database); err != nil {
			return err
		}

		if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
			return errors.Wrap(err, "saving module version in database object")
		}
	}

	if ingestionConsumer.Status.Ready && matchingConsumer.Status.Ready {
		if err := createDeployments(ctx, stack, reconciliation, database, authClient, ingestionConsumer, matchingConsumer, imageConfiguration); err != nil {
			return err
		}
	}

	return nil
}

func deleteLegacyResources(ctx Context, reconciliation *v1beta1.Reconciliation) error {
	// Delete legacy BrokerConsumer (name: <owner>-reconciliation, created with empty name param)
	legacyConsumer := &v1beta1.BrokerConsumer{}
	legacyConsumerName := fmt.Sprintf("%s-reconciliation", reconciliation.Name)
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: legacyConsumerName}, legacyConsumer); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		if err := ctx.GetClient().Delete(ctx, legacyConsumer); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	// Delete legacy Deployment (name: reconciliation-worker)
	legacyDeployment := &appsv1.Deployment{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Name:      "reconciliation-worker",
		Namespace: reconciliation.Namespace,
	}, legacyDeployment); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		if err := ctx.GetClient().Delete(ctx, legacyDeployment); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			WithOwn[*v1beta1.Reconciliation](&v1beta1.BrokerConsumer{}),
			WithOwn[*v1beta1.Reconciliation](&v1beta1.Database{}),
			WithOwn[*v1beta1.Reconciliation](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Reconciliation](&v1beta1.AuthClient{}),
			WithOwn[*v1beta1.Reconciliation](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Reconciliation](&batchv1.Job{}),
			WithOwn[*v1beta1.Reconciliation](&v1beta1.ResourceReference{}),
			WithWatchSettings[*v1beta1.Reconciliation](),
			WithWatchDependency[*v1beta1.Reconciliation](&v1beta1.Ledger{}),
			WithWatchDependency[*v1beta1.Reconciliation](&v1beta1.Payments{}),
			databases.Watch[*v1beta1.Reconciliation](),
			brokers.Watch[*v1beta1.Reconciliation](),
		),
	)
}
