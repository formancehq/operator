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
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/authclients"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
	"github.com/formancehq/operator/v3/internal/resources/brokertopics"
	"github.com/formancehq/operator/v3/internal/resources/databases"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/registries"
)

//+kubebuilder:rbac:groups=formance.com,resources=reconciliations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=reconciliations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=reconciliations/finalizers,verbs=update

func Reconcile(ctx Context, stack *v1beta1.Stack, reconciliation *v1beta1.Reconciliation, version string) error {
	v3Topology := usesV3Topology(version)
	database, err := databases.Create(ctx, stack, reconciliation)
	if err != nil {
		return err
	}

	var broker *v1beta1.Broker
	if v3Topology {
		candidate := &v1beta1.Broker{}
		hasBroker, err := GetIfExists(ctx, stack.Name, candidate)
		if err != nil {
			return err
		}
		if hasBroker {
			broker = candidate
			if !broker.Status.Ready {
				return NewPendingError().WithMessage("broker not ready")
			}
			topic, err := brokertopics.Create(ctx, stack, reconciliation, "reconciliation")
			if err != nil {
				return err
			}
			if !topic.Status.Ready {
				return NewPendingError().WithMessage("reconciliation broker topic not ready")
			}
		}
	}

	authClient, err := authclients.Create(ctx, stack, reconciliation, "reconciliation",
		authclients.WithScopes("ledger:read", "payments:read"))
	if err != nil {
		return err
	}

	if database.Status.Ready {

		imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "reconciliation", version)
		if err != nil {
			return errors.Wrap(err, "resolving image")
		}

		if databases.GetSavedModuleVersion(database) != version {

			if err := databases.Migrate(ctx, stack, reconciliation, imageConfiguration, database); err != nil {
				return err
			}

			if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
				return errors.Wrap(err, "saving module version in database object")
			}
		}

		if v3Topology {
			if err := createV3Deployments(ctx, stack, reconciliation, database, authClient, imageConfiguration, broker); err != nil {
				return err
			}
		} else {
			if err := deleteWorkerResources(ctx, stack.Name); err != nil {
				return err
			}
			if err := createDeployment(ctx, stack, reconciliation, database, authClient, imageConfiguration); err != nil {
				return err
			}
		}
	}

	if err := gatewayhttpapis.Create(ctx, reconciliation, gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck")); err != nil {
		return err
	}

	return nil
}

func usesV3Topology(version string) bool {
	return v1beta1.IsReconciliationV3(version)
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			Requirements(
				Require(&v1beta1.Ledger{}, VersionBefore(v1beta1.LedgerV3Version)),
			),
			WithOwn[*v1beta1.Reconciliation](&v1beta1.Database{}),
			WithOwn[*v1beta1.Reconciliation](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Reconciliation](&v1beta1.AuthClient{}),
			WithOwn[*v1beta1.Reconciliation](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Reconciliation](&batchv1.Job{}),
			WithOwn[*v1beta1.Reconciliation](&v1beta1.ResourceReference{}),
			WithWatchSettings[*v1beta1.Reconciliation](),
			brokers.Watch[*v1beta1.Reconciliation](),
			brokertopics.Watch[*v1beta1.Reconciliation]("reconciliation"),
			WithWatchDependency[*v1beta1.Reconciliation](&v1beta1.Payments{}),
		),
	)
}
