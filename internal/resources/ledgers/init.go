/*
Copyright 2022.

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

package ledgers

import (
	_ "embed"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/brokertopics"
	"github.com/formancehq/operator/v3/internal/resources/databases"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/jobs"
	"github.com/formancehq/operator/v3/internal/resources/registries"
)

//+kubebuilder:rbac:groups=formance.com,resources=ledgers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=ledgers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=ledgers/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

func Reconcile(ctx Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string) error {
	if isLedgerV3(version) {
		return reconcileV3(ctx, stack, ledger, version)
	}
	previewVersion, err := ledgerV3PreviewVersion(ctx, stack)
	if err != nil {
		return err
	}

	if ledgerV3ClusterAvailable {
		cluster, exists, err := getV3Cluster(ctx, stack)
		if err != nil {
			return err
		}
		if exists && !isLedgerV3Preview(cluster) {
			setLedgerV3Condition(ledger, metav1.ConditionFalse, "MigrationRequired", "A Ledger v3 Cluster exists; an explicit v3 to v2 migration is required")
			return NewPendingError().WithMessage("migration required before switching Ledger from v3 to v2")
		}
		if previewVersion == "" {
			if err := deleteLedgerV3Preview(ctx, stack); err != nil {
				return err
			}
		}
	}

	ledger.GetConditions().Delete(v1beta1.ConditionTypeMatch(ledgerV3ClusterReadyCondition))
	ledger.GetConditions().Delete(v1beta1.ConditionTypeMatch(ledgerV3PreviewReadyCondition))
	if err := reconcileLegacy(ctx, stack, ledger, version, previewVersion); err != nil {
		return err
	}
	if previewVersion != "" {
		return reconcileV3Preview(ctx, stack, ledger, previewVersion)
	}
	return nil
}

func reconcileLegacy(ctx Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version, previewVersion string) error {
	if previewVersion == "" {
		if err := DeleteIfExists[*v1beta1.GatewayGRPCAPI](ctx, GetResourceName(GetObjectName(stack.Name, "ledger"))); err != nil {
			return err
		}
	}

	database, err := databases.Create(ctx, stack, ledger)
	if err != nil {
		return err
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "ledger", version)
	if err != nil {
		return err
	}

	if err := gatewayhttpapis.Create(ctx, ledger,
		gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck"),
		gatewayhttpapis.WithRules(gatewayhttpapis.RuleSecured()),
	); err != nil {
		return err
	}

	hasDependency, err := HasDependency(ctx, stack.Name, &v1beta1.Search{})
	if err != nil {
		return err
	}
	if hasDependency {
		_, err = createReindexCronJob(ctx, ledger)
		if err != nil {
			return err
		}
	} else {
		err = deleteReindexCronJob(ctx, ledger)
		if err != nil {
			return err
		}
	}

	if !database.Status.Ready {
		return NewPendingError().WithMessage("database not ready")
	}

	if databases.GetSavedModuleVersion(database) != version {
		err := databases.Migrate(
			ctx,
			stack,
			ledger,
			imageConfiguration,
			database,
			jobs.Mutator(func(t *batchv1.Job) error {
				t.Spec.Template.Spec.Containers[0].Env = append(t.Spec.Template.Spec.Containers[0].Env, Env("STORAGE_POSTGRES_CONN_STRING", "$(POSTGRES_URI)"))

				return nil
			}),
		)
		if err != nil {
			if IsApplicationError(err) { // Start the ledger even if migrations are not terminated
				return installLedger(ctx, stack, ledger, database, imageConfiguration, version)
			}

			return err
		}
		if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
			return errors.Wrap(err, "saving module version in database object")
		}
	}

	return installLedger(ctx, stack, ledger, database, imageConfiguration, version)
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			NoRequirements(),
			WithOwn[*v1beta1.Ledger](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Ledger](&batchv1.Job{}),
			WithOwn[*v1beta1.Ledger](&corev1.Service{}),
			WithOwn[*v1beta1.Ledger](&v1beta1.GatewayGRPCAPI{}),
			WithOwn[*v1beta1.Ledger](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Ledger](&v1beta1.Database{}),
			WithOwn[*v1beta1.Ledger](&batchv1.CronJob{}),
			WithOwn[*v1beta1.Ledger](&corev1.ConfigMap{}),
			WithOwn[*v1beta1.Ledger](&v1beta1.BenthosStream{}),
			withLedgerV3ClusterWatch(),
			withLedgerConfigurationWatch(),
			WithWatchSettings[*v1beta1.Ledger](),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.Auth{}),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.MCP{}),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.Orchestration{}),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.Reconciliation{}),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.Search{}),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.TransactionPlane{}),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.Wallets{}),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.Webhooks{}),
			brokertopics.Watch[*v1beta1.Ledger]("ledger"),
			databases.Watch[*v1beta1.Ledger](),
		),
	)
}
