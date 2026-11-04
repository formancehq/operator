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
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/formancehq/go-libs/v5/pkg/types/collections"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/databases"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/registries"
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

	if databases.GetSavedModuleVersion(database) != version {
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

func watchCryptoKeySecret(ctx Context, secret *corev1.Secret) []reconcile.Request {
	// The `stack` field index on Auth is registered automatically for every
	// Dependent kind (internal/core/setup.go), so no extra index is needed.
	auths := &v1beta1.AuthList{}
	if err := ctx.GetClient().List(ctx, auths, client.MatchingFields{
		"stack": secret.GetNamespace(),
	}); err != nil {
		log.FromContext(ctx).Error(err, "listing auths on crypto key secret watch")
		return nil
	}

	// Watch-only filter: enqueue an Auth only when the event is relevant to
	// it, without ever mutating the Secret (no ownerReference adoption, no
	// rewrite). Two cases:
	//   - the default auth-crypto-key Secret (generated, owned, or pre-seeded
	//     unowned): every Auth of the stack is enqueued, so rotation,
	//     delete/recreate and bootstrap are all covered;
	//   - any other Secret: only Auths whose cryptoKeyFromSecret selector
	//     selects it, covering unlabeled external Secrets (no stack label
	//     requirement), missing->created, invalid->fixed and rename flows.
	requests := make([]reconcile.Request, 0, len(auths.Items))
	for i := range auths.Items {
		auth := &auths.Items[i]
		if secret.GetName() == cryptoKeySecretName ||
			(auth.Spec.CryptoKeyFromSecret != nil && auth.Spec.CryptoKeyFromSecret.Name == secret.GetName()) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: auth.GetName()},
			})
		}
	}
	return requests
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			NoRequirements(),
			WithOwn[*v1beta1.Auth](&appsv1.Deployment{}),
			WithOwn[*v1beta1.Auth](&v1beta1.GatewayHTTPAPI{}),
			WithOwn[*v1beta1.Auth](&v1beta1.Database{}),
			WithOwn[*v1beta1.Auth](&corev1.ConfigMap{}),
			WithOwn[*v1beta1.Auth](&corev1.Secret{}),
			// Bridge watch for the crypto key Secret: generated-mode secrets
			// are Auth-owned and already enqueued through WithOwn(Secret), but
			// an accepted pre-seeded (unowned) auth-crypto-key Secret would
			// never trigger a reconciliation when rotated. This watch-only
			// mapper enqueues the Auth(s) of the secret's stack on
			// create/update/delete; the reconciler remains the sole writer.
			// It never adopts the Secret (no ownerReference) and never
			// modifies it: regeneration happens only on genuine NotFound and
			// the existing validation path is unchanged.
			WithWatch[*v1beta1.Auth, *corev1.Secret](watchCryptoKeySecret),
			WithOwn[*v1beta1.Auth](&batchv1.Job{}),
			WithOwn[*v1beta1.Auth](&v1beta1.ResourceReference{}),
			WithWatchSettings[*v1beta1.Auth](),
			WithWatchDependencySpecOnly[*v1beta1.Auth](&v1beta1.Gateway{}),
			WithWatchDependency[*v1beta1.Auth](&v1beta1.AuthClient{}),
			databases.Watch[*v1beta1.Auth](),
		),
	)
}
