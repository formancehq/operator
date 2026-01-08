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

package payments

import (
	_ "embed"
	"net/http"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/jobs"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/pkg/errors"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=formance.com,resources=payments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=payments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=payments/finalizers,verbs=update

func Reconcile(ctx Context, stack *v1beta1.Stack, p *v1beta1.Payments, version string) error {

	database, err := databases.Create(ctx, stack, p)
	if err != nil {
		return err
	}

	if !database.Status.Ready {
		return NewPendingError().WithMessage("database not ready")
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "payments", version)
	if err != nil {
		return err
	}

	savedVersion := databases.GetSavedModuleVersion(database)
	if savedVersion != version {
		encryptionKey, err := getEncryptionKey(ctx, p)
		if err != nil {
			return err
		}

		if err := databases.Migrate(ctx, stack, p, imageConfiguration, database,
			jobs.WithEnvVars(Env("CONFIG_ENCRYPTION_KEY", encryptionKey)),
		); err != nil {
			return err
		}

		if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
			return errors.Wrap(err, "saving module version in database object")
		}
	}

	healthEndpoint := "_healthcheck"
	switch {
	case semver.IsValid(version) && semver.Compare(version, "v1.0.0-alpha") >= 0 &&
		semver.Compare(version, "v3.0.0") < 0:
		healthEndpoint = "_health"
		if err := createV2ReadDeployment(ctx, stack, p, database, imageConfiguration); err != nil {
			return err
		}

		if err := createV2ConnectorsDeployment(ctx, stack, p, database, imageConfiguration); err != nil {
			return err
		}
		if err := createGateway(ctx, stack, p); err != nil {
			return err
		}
	case semver.IsValid(version) && semver.Compare(version, "v3.0.0-beta.1") >= 0 &&
		semver.Compare(version, "v3.1.0-alpha.1") < 0:

		if err := uninstallPaymentsReadAndConnectors(ctx, stack); err != nil {
			return err
		}

		if err := createFullDeployment(ctx, stack, p, database, imageConfiguration); err != nil {
			return err
		}
	case !semver.IsValid(version) || semver.Compare(version, "v3.1.0-alpha.1") >= 0:
		if semver.Compare(savedVersion, "v3.0.0-beta.1") < 0 { // If we are running an update from <3.0.0-beta.1 we need to delete the old deployments
			if err := uninstallPaymentsReadAndConnectors(ctx, stack); err != nil {
				return err
			}
		}
		if savedVersion != version { // We need to make sure we're currently updating, if not it'll loop creating and deleting the new pods
			if err := deleteDeployment(ctx, stack, "payments-worker"); err != nil {
				return err
			}
		}
		// check if all deleted
		if err := createFullDeployment(ctx, stack, p, database, imageConfiguration); err != nil {
			return err
		}
	}

	if err := gatewayhttpapis.Create(ctx, p,
		gatewayhttpapis.WithHealthCheckEndpoint(healthEndpoint),
		gatewayhttpapis.WithRules(
			v1beta1.GatewayHTTPAPIRule{
				Path:    "/connectors/webhooks",
				Methods: []string{http.MethodPost},
				Secured: true,
			},
			gatewayhttpapis.RuleSecured(),
		)); err != nil {
		return err
	}

	return nil
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			WithFinalizer[*v1beta1.Payments]("clean-payments", Clean),
			WithOwn[*v1beta1.Payments](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Payments](&corev1.Service{}),
			WithOwn[*v1beta1.Payments](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Payments](&batchv1.Job{}),
			WithOwn[*v1beta1.Payments](&corev1.ConfigMap{}),
			WithOwn[*v1beta1.Payments](&v1beta1.BenthosStream{}),
			WithWatchSettings[*v1beta1.Payments](),
			WithWatchDependency[*v1beta1.Payments](&v1beta1.Search{}),
			databases.Watch[*v1beta1.Payments](),
			brokertopics.Watch[*v1beta1.Payments]("payments"),
		),
	)
}
