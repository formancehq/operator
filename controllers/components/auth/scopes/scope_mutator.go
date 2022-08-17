package scopes

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

var scopeFinalizer = finalizerutil.New("scopes.auth.components.formance.com/finalizer")

// +kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/finalizers,verbs=update

type Mutator struct {
	client  client.Client
	scheme  *runtime.Scheme
	factory pkgInternal.APIFactory
}

func (s Mutator) SetupWithBuilder(builder *ctrl.Builder) {}

func (s Mutator) Mutate(ctx context.Context, actualK8SScope *authcomponentsv1beta1.Scope) (*ctrl.Result, error) {
	api := s.factory.Create(actualK8SScope)

	// Handle finalizer
	if isHandledByFinalizer, err := scopeFinalizer.Handle(ctx, s.client, actualK8SScope, func() error {
		// If the scope was created auth server side, we have to remove it
		if actualK8SScope.IsCreatedOnAuthServer() {
			return pkgError.Wrap(api.DeleteScope(ctx, actualK8SScope.Status.AuthServerID), "Deleting scope")
		}
		return nil
	}); err != nil || isHandledByFinalizer {
		return nil, err
	}

	SetProgressing(actualK8SScope)

	var (
		err                             error
		actualAuthServerScope           *authclient.Scope
		authServerScopeExpectedMetadata = map[string]string{
			"namespace": actualK8SScope.Namespace,
			"name":      actualK8SScope.Name,
		}
	)

	// Scope already created auth server side
	if actualK8SScope.IsCreatedOnAuthServer() {
		// Scope can have been manually deleted
		if actualAuthServerScope, err = api.ReadScope(ctx, actualK8SScope.Status.AuthServerID); err != nil && err != pkgInternal.ErrNotFound {
			return nil, pkgError.Wrap(err, "Reading scope auth server side")
		}
		if actualAuthServerScope != nil { // If found, check the label and update if required
			if actualAuthServerScope.Label != actualK8SScope.Spec.Label {
				if err := api.UpdateScope(ctx, actualAuthServerScope.Id, actualK8SScope.Spec.Label,
					authServerScopeExpectedMetadata); err != nil {
					return nil, pkgError.Wrap(err, "Updating scope auth server side")
				}
			}
		} else {
			// Scope was deleted
			actualK8SScope.ClearAuthServerID()
		}
	}

	// Still not created
	if !actualK8SScope.IsCreatedOnAuthServer() {
		// As it could be the status update of the reconciliation which could have been fail
		// the scope can exist auth server side, so try to find it using metadata
		if actualAuthServerScope, err = api.
			ReadScopeByMetadata(ctx, authServerScopeExpectedMetadata); err != nil && err != pkgInternal.ErrNotFound {
			return nil, pkgError.Wrap(err, "Reading scope by metadata")
		}

		// If the scope is not found auth server side, we can create the scope
		if actualAuthServerScope == nil {
			if actualAuthServerScope, err = api.CreateScope(ctx, actualK8SScope.Spec.Label, authServerScopeExpectedMetadata); err != nil {
				return nil, pkgError.Wrap(err, "Creating scope auth server side")
			}
		}
		actualK8SScope.Status.AuthServerID = actualAuthServerScope.Id
	}

	needRequeue := false
	transientScopeIds := make([]string, 0)
	for _, transientScopeName := range actualK8SScope.Spec.Transient {
		transientK8SScope := &authcomponentsv1beta1.Scope{}
		if err := s.client.Get(ctx, types.NamespacedName{
			Name:      transientScopeName,
			Namespace: actualK8SScope.Namespace,
		}, transientK8SScope); err != nil {
			if !errors.IsNotFound(err) {
				return nil, pkgError.Wrap(err, "Reading scope k8s side")
			}
			// The transient scope is not created, requeue is needed
			log.FromContext(ctx).Info("Scope not found, need requeue", "scope", transientScopeName)
			needRequeue = true
			continue
		}

		if !transientK8SScope.IsInTransient(actualAuthServerScope) { // Transient scope not found auth server side
			if err = api.AddTransientScope(ctx, actualK8SScope.Status.AuthServerID, transientK8SScope.Status.AuthServerID); err != nil {
				return nil, pkgError.Wrap(err, "Adding transient scope auth server side")
			}
			actualK8SScope.SetRegisteredTransientScope(transientK8SScope)
		}
		transientScopeIds = append(transientScopeIds, transientK8SScope.Status.AuthServerID)
	}

	extraTransientScopes := Filter(actualAuthServerScope.Transient, NotIn(transientScopeIds...))
	for _, extraScope := range extraTransientScopes {
		if err = api.RemoveTransientScope(ctx, actualAuthServerScope.Id, extraScope); err != nil {
			return nil, pkgError.Wrap(err, "Removing transient scope auth server side")
		}
	}

	if !needRequeue {
		SetReady(actualK8SScope)
	}

	return &ctrl.Result{
		Requeue: needRequeue,
	}, nil
}

var _ internal.Mutator[*authcomponentsv1beta1.Scope] = &Mutator{}

func NewMutator(
	client client.Client,
	scheme *runtime.Scheme,
	apiFactory pkgInternal.APIFactory,
) internal.Mutator[*authcomponentsv1beta1.Scope] {
	return &Mutator{
		client:  client,
		scheme:  scheme,
		factory: apiFactory,
	}
}

var DefaultApiFactory = pkgInternal.DefaultApiFactory
