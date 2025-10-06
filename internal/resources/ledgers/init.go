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
	"fmt"
	"github.com/formancehq/operator/internal/resources/jobs"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/benthosstreams"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/search/benthos"
	"github.com/pkg/errors"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=formance.com,resources=ledgers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=ledgers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=ledgers/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

func Reconcile(ctx Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string) error {
	database, err := databases.Create(ctx, stack, ledger)
	if err != nil {
		return err
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "ledger", version)
	if err != nil {
		return err
	}

	if err := gatewayhttpapis.Create(ctx, ledger, gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck")); err != nil {
		return err
	}

	isV2 := false
	if !semver.IsValid(version) || semver.Compare(version, "v2.0.0-alpha") > 0 {
		isV2 = true
	}

	if err := benthosstreams.LoadFromFileSystem(ctx, benthos.Streams, ledger, "streams/ledger", "ingestion"); err != nil {
		return err
	}

	streamsVersion := "v1.0.0"
	if isV2 {
		streamsVersion = "v2.0.0"
	}
	if err := benthosstreams.LoadFromFileSystem(ctx, reindexStreams, ledger, fmt.Sprintf("assets/reindex/%s", streamsVersion), "reindex"); err != nil {
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

	if isV2 && databases.GetSavedModuleVersion(database) != version {
		err := databases.Migrate(
			ctx,
			stack,
			ledger,
			imageConfiguration,
			database,
			jobs.Mutator(func(t *batchv1.Job) error {
				if IsLower(version, "v2.0.0-rc.6") {
					t.Spec.Template.Spec.Containers[0].Command = []string{"buckets", "upgrade-all"}
				}
				t.Spec.Template.Spec.Containers[0].Env = append(t.Spec.Template.Spec.Containers[0].Env, Env("STORAGE_POSTGRES_CONN_STRING", "$(POSTGRES_URI)"))

				return nil
			}),
			jobs.PreCreate(func() error {
				list := &appsv1.DeploymentList{}
				if err := ctx.GetClient().List(ctx, list, client.InNamespace(stack.Name)); err != nil {
					return err
				}

				for _, item := range list.Items {
					if controller := metav1.GetControllerOf(&item); controller != nil && controller.UID == ledger.GetUID() {
						if err := ctx.GetClient().Delete(ctx, &item); err != nil {
							return err
						}
					}
				}

				return nil
			}),
		)
		if err != nil {
			isV2_2 := !semver.IsValid(version) || semver.Compare(version, "v2.2.0-alpha") > 0
			if !isV2_2 {
				return err
			}

			if IsApplicationError(err) { // Start the ledger even if migrations are not terminated
				return installLedger(ctx, stack, ledger, database, imageConfiguration, version, isV2)
			}

			return err
		}
		if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
			return errors.Wrap(err, "saving module version in database object")
		}
	}

	return installLedger(ctx, stack, ledger, database, imageConfiguration, version, isV2)
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			WithOwn[*v1beta1.Ledger](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Ledger](&batchv1.Job{}),
			WithOwn[*v1beta1.Ledger](&corev1.Service{}),
			WithOwn[*v1beta1.Ledger](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Ledger](&v1beta1.Database{}),
			WithOwn[*v1beta1.Ledger](&batchv1.CronJob{}),
			WithOwn[*v1beta1.Ledger](&corev1.ConfigMap{}),
			WithOwn[*v1beta1.Ledger](&v1beta1.BenthosStream{}),
			WithWatchSettings[*v1beta1.Ledger](),
			WithWatchDependency[*v1beta1.Ledger](&v1beta1.Search{}),
			brokertopics.Watch[*v1beta1.Ledger]("ledger"),
			databases.Watch[*v1beta1.Ledger](),
		),
	)
}
