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

	"github.com/pkg/errors"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/formancehq/search/benthos"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/benthosstreams"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/jobs"
	"github.com/formancehq/operator/internal/resources/registries"
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

	if databases.GetSavedModuleVersion(database) != version {
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

	healthEndpoint := "_health"
	switch {
	case semver.IsValid(version) && semver.Compare(version, "v1.0.0-alpha") < 0:
		if err := createFullDeployment(ctx, stack, p, database, imageConfiguration, false); err != nil {
			return err
		}
	case semver.IsValid(version) && semver.Compare(version, "v1.0.0-alpha") >= 0 &&
		semver.Compare(version, "v3.0.0") < 0:
		if err := createReadDeployment(ctx, stack, p, database, imageConfiguration); err != nil {
			return err
		}

		if err := createConnectorsDeployment(ctx, stack, p, database, imageConfiguration); err != nil {
			return err
		}
		if err := createGateway(ctx, stack, p); err != nil {
			return err
		}
	case !semver.IsValid(version) || semver.Compare(version, "v3.0.0-beta.1") >= 0:
		healthEndpoint = "_healthcheck"
		if err := uninstallPaymentsReadAndConnectors(ctx, stack); err != nil {
			return err
		}

		if err := createFullDeployment(ctx, stack, p, database, imageConfiguration, true); err != nil {
			return err
		}
	}

	if err := benthosstreams.LoadFromFileSystem(ctx, benthos.Streams, p, "streams/payments", "ingestion"); err != nil {
		return err
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
