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

package clients

import (
	"context"
	"fmt"

	"github.com/numary/auth/authclient"
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/auth.components/v1beta1"
	. "github.com/numary/formance-operator/pkg/collectionutil"
	"github.com/numary/formance-operator/pkg/finalizerutil"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var clientFinalizer = finalizerutil.New("clients.auth.components.formance.com/finalizer")

var ErrNotFound = fmt.Errorf("client not found")

// ClientReconciler reconciles a Client object
type ClientReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	API    ClientAPI
}

//+kubebuilder:rbac:groups=auth.components.formance.com,resources=clients,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=auth.components.formance.com,resources=clients/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=auth.components.formance.com,resources=clients/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *ClientReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	logger := log.FromContext(ctx)
	logger.Info("Start reconciliation")
	defer func() {
		logger.Info("Reconciliation terminated")
	}()

	actualClient := &authcomponentsv1beta1.Client{}
	if err := r.Get(ctx, req.NamespacedName, actualClient); err != nil {
		return ctrl.Result{}, err
	}

	updatedClient := actualClient.DeepCopy()

	result, reconcileError := r.reconcile(ctx, updatedClient)
	if reconcileError != nil {
		log.FromContext(ctx).Error(reconcileError, "Reconciling error")
	}

	if err := r.Status().Update(ctx, updatedClient); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Status updated", "generation", updatedClient.Generation)
	if result != nil {
		return *result, reconcileError
	}

	return ctrl.Result{}, reconcileError
}

func (r *ClientReconciler) reconcile(ctx context.Context, actualK8SClient *authcomponentsv1beta1.Client) (*ctrl.Result, error) {

	logger := log.FromContext(ctx)

	actualK8SClient.Progressing()

	// Handle finalizer
	if isHandledByFinalizer, err := clientFinalizer.Handle(ctx, r.Client, actualK8SClient, func() error {
		// If the scope was created auth server side, we have to remove it
		if actualK8SClient.IsCreatedOnAuthServer() {
			return r.API.DeleteClient(ctx, actualK8SClient.Status.AuthServerID)
		}
		return nil
	}); err != nil || isHandledByFinalizer {
		return nil, err
	}

	// Assert finalizer is properly installed on the object
	if err := clientFinalizer.AssertIsInstalled(ctx, r.Client, actualK8SClient); err != nil {
		return nil, err
	}

	var (
		actualAuthServerClient           *authclient.Client
		err                              error
		authServerClientExpectedMetadata = map[string]string{
			"namespace": actualK8SClient.Namespace,
			"name":      actualK8SClient.Name,
		}
		expectedClientOptions = authclient.ClientOptions{
			Public:                 &actualK8SClient.Spec.Public,
			RedirectUris:           actualK8SClient.Spec.RedirectUris,
			Description:            actualK8SClient.Spec.Description,
			Name:                   actualK8SClient.Name,
			PostLogoutRedirectUris: actualK8SClient.Spec.PostLogoutRedirectUris,
			Metadata:               &authServerClientExpectedMetadata,
		}
	)
	// Client already created auth server side
	if actualK8SClient.IsCreatedOnAuthServer() {
		// Client can have been manually deleted
		if actualAuthServerClient, err = r.API.ReadClient(ctx, actualK8SClient.Status.AuthServerID); err != nil && err != ErrNotFound {
			return nil, err
		}
		if actualAuthServerClient != nil {
			if !actualK8SClient.Match(actualAuthServerClient) {
				logger.Info("Detect divergence between auth server and k8s information, update auth server resource")
				if err := r.API.UpdateClient(ctx, actualAuthServerClient.Id, expectedClientOptions); err != nil {
					return nil, err
				}
			}
		} else {
			// Client was deleted
			logger.Info("ID saved in status does not match any clients auth server side")
			actualK8SClient.ClearAuthServerID()
		}
	}

	// Still not created
	if !actualK8SClient.IsCreatedOnAuthServer() {
		// As it could be the status update of the reconciliation which could have been fail
		// the client can exist auth server side, so try to find it
		if actualAuthServerClient, err = r.API.
			ReadClientByMetadata(ctx, authServerClientExpectedMetadata); err != nil && err != ErrNotFound {
			return nil, err
		}

		// If the scope is not found auth server side, we can create it
		if actualAuthServerClient == nil {
			logger.Info("Create auth server client")
			if actualAuthServerClient, err = r.API.CreateClient(ctx, expectedClientOptions); err != nil {
				return nil, err
			}
		} else {
			logger.Info("Found auth server client using metadata, use it")
		}
		actualK8SClient.SetClientCreated(actualAuthServerClient.Id)
	}

	needRequeue := false
	scopeIds := make([]string, 0)
	for _, k8sScopeName := range actualK8SClient.Spec.Scopes {
		logger = logger.WithValues("scope", k8sScopeName)

		logger.Info("Checking scope presence on auth server client")
		scope := &authcomponentsv1beta1.Scope{}
		err := r.Get(ctx, types.NamespacedName{
			Namespace: actualK8SClient.Namespace,
			Name:      k8sScopeName,
		}, scope)
		if err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Scope not found locally")
			return nil, err
		}
		if err != nil {
			logger.Info("Scope used by client not found, requeue")
			needRequeue = true // If the scope does not exist, we simply requeue
			continue
		}
		if !scope.IsCreatedOnAuthServer() {
			logger.Info("Scope used by client not synchronized, requeue")
			needRequeue = true
			continue
		}
		scopeIds = append(scopeIds, scope.Status.AuthServerID)
		if v := First(actualAuthServerClient.Scopes, Equal(scope.Status.AuthServerID)); v != nil {
			logger.Info("Scope already configured")
			// Scope already on client
			continue
		}

		if err := r.API.AddScopeToClient(ctx, actualAuthServerClient.Id, scope.Status.AuthServerID); err != nil {
			logger.Error(err, "Adding scope to the auth server client")
			return nil, err
		}

		actualK8SClient.SetScopeSynchronized(scope)
		logger.Info("Scope added to the client")
	}

	extraScopes := Filter(actualAuthServerClient.Scopes, NotIn(scopeIds...))
	for _, extraScope := range extraScopes {
		logger.Info("Delete scope from the client as it is not needed anymore", "scope", extraScope)
		if err := r.API.DeleteScopeFromClient(ctx, actualAuthServerClient.Id, extraScope); err != nil {
			return nil, err
		}
		actualK8SClient.SetScopesRemoved(extraScope)
	}

	if !needRequeue {
		actualK8SClient.Ready()
	}

	return &ctrl.Result{
		Requeue: needRequeue,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClientReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authcomponentsv1beta1.Client{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

func NewReconciler(client client.Client, scheme *runtime.Scheme, api ClientAPI) *ClientReconciler {
	return &ClientReconciler{
		Client: client,
		Scheme: scheme,
		API:    api,
	}
}
