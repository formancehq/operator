package clients

import (
	"context"

	"github.com/numary/auth/authclient"
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	pkgInternal "github.com/numary/formance-operator/controllers/components/auth/internal"
	"github.com/numary/formance-operator/internal"
	. "github.com/numary/formance-operator/internal/collectionutil"
	"github.com/numary/formance-operator/pkg/finalizerutil"
	pkgError "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var clientFinalizer = finalizerutil.New("clients.auth.components.formance.com/finalizer")

// +kubebuilder:rbac:groups=auth.components.formance.com,resources=clients,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=auth.components.formance.com,resources=clients/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=auth.components.formance.com,resources=clients/finalizers,verbs=update

type Mutator struct {
	client  client.Client
	scheme  *runtime.Scheme
	factory pkgInternal.APIFactory
}

func (c Mutator) SetupWithBuilder(builder *ctrl.Builder) {}

func (r Mutator) Mutate(ctx context.Context, actualK8SClient *authcomponentsv1beta1.Client) (*ctrl.Result, error) {
	logger := log.FromContext(ctx)

	SetProgressing(actualK8SClient)

	api := r.factory.Create(actualK8SClient)

	// Handle finalizer
	if isHandledByFinalizer, err := clientFinalizer.Handle(ctx, r.client, actualK8SClient, func() error {
		// If the scope was created auth server side, we have to remove it
		if actualK8SClient.IsCreatedOnAuthServer() {
			return pkgError.Wrap(api.DeleteClient(ctx, actualK8SClient.Status.AuthServerID),
				"Deleting client auth server side")
		}
		return nil
	}); err != nil || isHandledByFinalizer {
		return nil, err
	}

	// Assert finalizer is properly installed on the object
	if err := clientFinalizer.AssertIsInstalled(ctx, r.client, actualK8SClient); err != nil {
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
		if actualAuthServerClient, err = api.ReadClient(ctx, actualK8SClient.Status.AuthServerID); err != nil && err != pkgInternal.ErrNotFound {
			return nil, pkgError.Wrap(err, "Reading client auth server side")
		}
		if actualAuthServerClient != nil {
			if !actualK8SClient.Match(actualAuthServerClient) {
				logger.Info("Detect divergence between auth server and k8s information, update auth server resource")
				if err := api.UpdateClient(ctx, actualAuthServerClient.Id, expectedClientOptions); err != nil {
					return nil, pkgError.Wrap(err, "Updating client auth server side")
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
		logger.Info("Auth server ID not defined, try to retrieve by metadata")
		// As it could be the status update of the reconciliation which could have been fail
		// the client can exist auth server side, so try to find it
		if actualAuthServerClient, err = api.
			ReadClientByMetadata(ctx, authServerClientExpectedMetadata); err != nil && err != pkgInternal.ErrNotFound {
			return nil, pkgError.Wrap(err, "Reading client by metadata")
		}

		// If the client is not found auth server side, we can create it
		if actualAuthServerClient == nil {
			logger.Info("Create auth server client")
			if actualAuthServerClient, err = api.CreateClient(ctx, expectedClientOptions); err != nil {
				return nil, pkgError.Wrap(err, "Creating client")
			}
		} else {
			logger.Info("Found auth server client using metadata, use it", "id", actualAuthServerClient.Id)
		}
		actualK8SClient.SetClientCreated(actualAuthServerClient.Id)
	}

	needRequeue := false
	scopeIds := make([]string, 0)
	for _, k8sScopeName := range actualK8SClient.Spec.Scopes {
		logger = logger.WithValues("scope", k8sScopeName)

		logger.Info("Checking scope presence on auth server client")
		scope := &authcomponentsv1beta1.Scope{}
		err := r.client.Get(ctx, types.NamespacedName{
			Namespace: actualK8SClient.Namespace,
			Name:      k8sScopeName,
		}, scope)
		if err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Scope not found locally")
			return nil, pkgError.Wrap(err, "Reading local scope")
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

		if err := api.AddScopeToClient(ctx, actualAuthServerClient.Id, scope.Status.AuthServerID); err != nil {
			logger.Error(err, "Adding scope to the auth server client")
			return nil, pkgError.Wrap(err, "Adding scope to the client auth server side")
		}

		actualK8SClient.SetScopeSynchronized(scope)
		logger.Info("Scope added to the client")
	}

	extraScopes := Filter(actualAuthServerClient.Scopes, NotIn(scopeIds...))
	for _, extraScope := range extraScopes {
		logger.Info("Delete scope from the client as it is not needed anymore", "scope", extraScope)
		if err := api.DeleteScopeFromClient(ctx, actualAuthServerClient.Id, extraScope); err != nil {
			return nil, pkgError.Wrap(err, "Deleting scope from client auth server side")
		}
		actualK8SClient.SetScopesRemoved(extraScope)
	}

	if !needRequeue {
		SetReady(actualK8SClient)
	}

	return &ctrl.Result{
		Requeue: needRequeue,
	}, nil
}

var _ internal.Mutator[*authcomponentsv1beta1.Client] = &Mutator{}

func NewMutator(
	client client.Client,
	scheme *runtime.Scheme,
	factory pkgInternal.APIFactory,
) internal.Mutator[*authcomponentsv1beta1.Client] {
	return &Mutator{
		client:  client,
		scheme:  scheme,
		factory: factory,
	}
}

var DefaultApiFactory = pkgInternal.DefaultApiFactory
