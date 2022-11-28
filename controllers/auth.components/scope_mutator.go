package auth_components

import (
	"context"

	"github.com/numary/auth/authclient"
	authcomponentsv1beta2 "github.com/numary/operator/apis/auth.components/v1beta2"
	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	"github.com/numary/operator/controllers/components"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/typeutils"
	pkgError "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var scopeFinalizer = controllerutils.New("scopes.auth.components.formance.com/finalizer")

// +kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/finalizers,verbs=update

type ScopesMutator struct {
	client  client.Client
	scheme  *runtime.Scheme
	factory components.APIFactory
}

func (s ScopesMutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error { return nil }

func (s ScopesMutator) Mutate(ctx context.Context, actualK8SScope *authcomponentsv1beta2.Scope) (*ctrl.Result, error) {
	api := s.factory.Create(actualK8SScope)

	// Handle finalizer
	if isHandledByFinalizer, err := scopeFinalizer.Handle(ctx, s.client, actualK8SScope, func() error {
		// If the scope was created auth server side, we have to remove it
		if actualK8SScope.IsCreatedOnAuthServer() {
			err := api.DeleteScope(ctx, actualK8SScope.Status.AuthServerID)
			if err == components.ErrNotFound {
				return nil
			}
			return pkgError.Wrap(err, "Deleting scope")
		}
		return nil
	}); err != nil || isHandledByFinalizer {
		return controllerutils.Requeue(), err
	}

	apisv1beta1.SetProgressing(actualK8SScope)

	if err := controllerutils.DefineOwner(ctx, s.client, s.scheme, actualK8SScope, types.NamespacedName{
		Namespace: actualK8SScope.GetNamespace(),
		Name:      actualK8SScope.GetNamespace() + "-" + actualK8SScope.Spec.AuthServerReference,
	}, &componentsv1beta2.Auth{}); err != nil {
		return controllerutils.Requeue(), err
	}

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
		if actualAuthServerScope, err = api.ReadScope(ctx, actualK8SScope.Status.AuthServerID); err != nil && err != components.ErrNotFound {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Reading scope auth server side")
		}
		if actualAuthServerScope != nil { // If found, check the label and update if required
			if actualAuthServerScope.Label != actualK8SScope.Spec.Label {
				if err := api.UpdateScope(ctx, actualAuthServerScope.Id, actualK8SScope.Spec.Label,
					authServerScopeExpectedMetadata); err != nil {
					return controllerutils.Requeue(), pkgError.Wrap(err, "Updating scope auth server side")
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
			ReadScopeByMetadata(ctx, authServerScopeExpectedMetadata); err != nil && err != components.ErrNotFound {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Reading scope by metadata")
		}

		// If the scope is not found auth server side, we can create the scope
		if actualAuthServerScope == nil {
			if actualAuthServerScope, err = api.CreateScope(ctx, actualK8SScope.Spec.Label, authServerScopeExpectedMetadata); err != nil {
				return controllerutils.Requeue(), pkgError.Wrap(err, "Creating scope auth server side")
			}
		}
		actualK8SScope.Status.AuthServerID = actualAuthServerScope.Id
	}

	needRequeue := false
	transientScopeIds := make([]string, 0)
	for _, transientScopeName := range actualK8SScope.Spec.Transient {
		transientK8SScope := &authcomponentsv1beta2.Scope{}
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
				return controllerutils.Requeue(), pkgError.Wrap(err, "Adding transient scope auth server side")
			}
			actualK8SScope.SetRegisteredTransientScope(transientK8SScope)
		}
		transientScopeIds = append(transientScopeIds, transientK8SScope.Status.AuthServerID)
	}

	extraTransientScopes := Filter(actualAuthServerScope.Transient, NotIn(transientScopeIds...))
	for _, extraScope := range extraTransientScopes {
		if err = api.RemoveTransientScope(ctx, actualAuthServerScope.Id, extraScope); err != nil {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Removing transient scope auth server side")
		}
	}

	if !needRequeue {
		apisv1beta1.SetReady(actualK8SScope)
		return nil, nil
	}

	return controllerutils.Requeue(), nil
}

var _ controllerutils.Mutator[*authcomponentsv1beta2.Scope] = &ScopesMutator{}

func NewScopesMutator(
	client client.Client,
	scheme *runtime.Scheme,
	apiFactory components.APIFactory,
) controllerutils.Mutator[*authcomponentsv1beta2.Scope] {
	return &ScopesMutator{
		client:  client,
		scheme:  scheme,
		factory: apiFactory,
	}
}
