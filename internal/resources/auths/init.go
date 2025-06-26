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

package auths

import (
	"github.com/davecgh/go-spew/spew"
	. "github.com/formancehq/go-libs/v2/collectionutils"
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:rbac:groups=formance.com,resources=auths,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=auths/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=auths/finalizers,verbs=update

func checkAuthClientsReconciliation(ctx Context, auth *v1beta1.Auth) ([]*v1beta1.AuthClient, error) {
	condition := v1beta1.NewCondition("AuthClientsReconciliation", auth.Generation).SetMessage("AuthClientsReady")
	defer func() {
		auth.GetConditions().AppendOrReplace(*condition, v1beta1.AndConditions(
			v1beta1.ConditionTypeMatch("AuthClientsReconciliation"),
		))
	}()
	authClients := make([]*v1beta1.AuthClient, 0)
	if err := GetAllStackDependencies(ctx, auth.Spec.Stack, &authClients); err != nil {
		return nil, err
	}

	for _, client := range authClients {
		if !client.Status.Ready {
			condition.SetMessage("OneOfAuthClientsNotReady")
			condition.SetStatus(v1.ConditionFalse)
		}
	}

	return authClients, nil
}

func Reconcile(ctx Context, stack *v1beta1.Stack, auth *v1beta1.Auth, version string) error {

	authClients, err := checkAuthClientsReconciliation(ctx, auth)
	if err != nil {
		return err
	}

	configMap, err := createConfiguration(ctx, stack, auth, authClients, version)
	if err != nil {
		return errors.Wrap(err, "creating configuration")
	}

	database, err := databases.Create(ctx, stack, auth)
	if err != nil {
		return errors.Wrap(err, "creating database")
	}

	if !database.Status.Ready {
		return NewPendingError().WithMessage("database is not ready")
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "auth", version)
	if err != nil {
		return errors.Wrap(err, "resolving image configuration")
	}
	spew.Dump(imageConfiguration)

	if IsGreaterOrEqual(version, "v2.0.0-rc.5") && databases.GetSavedModuleVersion(database) != version {
		if err := databases.Migrate(ctx, stack, auth, imageConfiguration, database); err != nil {
			return err
		}

		if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
			return errors.Wrap(err, "saving module version in database object")
		}
	}

	if err := createDeployment(ctx, stack, auth, database, configMap, imageConfiguration, version, authClients); err != nil {
		return errors.Wrap(err, "creating deployment")
	}

	if err := gatewayhttpapis.Create(ctx, auth, gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck"), gatewayhttpapis.WithRules(gatewayhttpapis.RuleUnsecured())); err != nil {
		return errors.Wrap(err, "creating http api")
	}

	auth.Status.Clients = Map(authClients, (*v1beta1.AuthClient).GetName)

	return nil
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			WithOwn[*v1beta1.Auth](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Auth](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Auth](&v1beta1.Database{}),
			WithOwn[*v1beta1.Auth](&corev1.ConfigMap{}),
			WithOwn[*v1beta1.Auth](&batchv1.Job{}),
			WithOwn[*v1beta1.Auth](&v1beta1.ResourceReference{}),
			WithWatchSettings[*v1beta1.Auth](),
			WithWatchDependency[*v1beta1.Auth](&v1beta1.AuthClient{}),
			databases.Watch[*v1beta1.Auth](),
		),
	)
}
