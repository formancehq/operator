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
package scopes

import (
	"context"

	"github.com/numary/auth/authclient"
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	"github.com/numary/formance-operator/controllers/components/auth/internal"
	. "github.com/numary/formance-operator/pkg/collectionutil"
	"github.com/numary/formance-operator/pkg/finalizerutil"
	pkgError "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ScopeReconciler reconciles a Scope object
type ScopeReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	factory internal.APIFactory
}

var scopeFinalizer = finalizerutil.New("scopes.auth.components.formance.com/finalizer")

//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/finalizers,verbs=update

func (r *ScopeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	actualScope := &authcomponentsv1beta1.Scope{}
	if err := r.Get(ctx, req.NamespacedName, actualScope); err != nil {
		return ctrl.Result{}, err
	}

	updatedScope := actualScope.DeepCopy()

	result, reconcileError := r.reconcile(ctx, updatedScope)
	if reconcileError != nil {
		log.FromContext(ctx).Error(reconcileError, "Reconciling")
	}
	if patchErr := r.Status().Update(ctx, updatedScope); patchErr != nil {
		return ctrl.Result{}, patchErr
	}

	if result != nil {
		return *result, nil
	}

	return ctrl.Result{
		Requeue: reconcileError != nil,
	}, nil
}

func (r *ScopeReconciler) reconcile(ctx context.Context, actualK8SScope *authcomponentsv1beta1.Scope) (*ctrl.Result, error) {

	api := r.factory.Create(actualK8SScope)

	// Handle finalizer
	if isHandledByFinalizer, err := scopeFinalizer.Handle(ctx, r.Client, actualK8SScope, func() error {
		// If the scope was created auth server side, we have to remove it
		if actualK8SScope.IsCreatedOnAuthServer() {
			return pkgError.Wrap(api.DeleteScope(ctx, actualK8SScope.Status.AuthServerID), "Deleting scope")
		}
		return nil
	}); err != nil || isHandledByFinalizer {
		return nil, err
	}

	actualK8SScope.Progress()

	// Assert finalizer is properly installed on the object
	if err := scopeFinalizer.AssertIsInstalled(ctx, r.Client, actualK8SScope); err != nil {
		return nil, err
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
		if actualAuthServerScope, err = api.ReadScope(ctx, actualK8SScope.Status.AuthServerID); err != nil && err != internal.ErrNotFound {
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
			ReadScopeByMetadata(ctx, authServerScopeExpectedMetadata); err != nil && err != internal.ErrNotFound {
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
		if err := r.Get(ctx, types.NamespacedName{
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
		actualK8SScope.StopProgression()
	}

	return &ctrl.Result{
		Requeue: needRequeue,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScopeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authcomponentsv1beta1.Scope{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

func NewReconciler(c client.Client, scheme *runtime.Scheme, factory internal.APIFactory) *ScopeReconciler {
	return &ScopeReconciler{
		Client:  c,
		Scheme:  scheme,
		factory: factory,
	}
}

var DefaultApiFactory = internal.DefaultApiFactory
