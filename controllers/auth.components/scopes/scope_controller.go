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
	"reflect"

	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/auth.components/v1beta1"
	"github.com/numary/formance-operator/pkg/finalizerutil"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ScopeReconciler reconciles a Scope object
type ScopeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	API    ScopeAPI
}

var scopeFinalizer = finalizerutil.New("scopes.auth.components.formance.com/finalizer")

//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *ScopeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	actualScope := &authcomponentsv1beta1.Scope{}
	if err := r.Get(ctx, req.NamespacedName, actualScope); err != nil {
		return ctrl.Result{}, err
	}

	updatedScope := actualScope.DeepCopy()

	reconcileError := r.reconcile(ctx, updatedScope)
	if reconcileError != nil {
		updatedScope.SetSynchronizationError(reconcileError)
	} else {
		updatedScope.SetSynchronized()
	}

	if !reflect.DeepEqual(updatedScope, actualScope) {
		if patchErr := r.Status().Update(ctx, updatedScope); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
	}

	return ctrl.Result{}, reconcileError
}

func (r *ScopeReconciler) reconcile(ctx context.Context, actualScope *authcomponentsv1beta1.Scope) error {

	// Handle finalizer
	if isHandledByFinalizer, err := scopeFinalizer.Handle(ctx, r.Client, actualScope, func() error {
		// If the scope was created auth server side, we have to remove it
		if actualScope.IsCreatedOnAuthServer() {
			return r.API.DeleteScope(ctx, actualScope.Status.AuthServerID)
		}
		return nil
	}); err != nil || isHandledByFinalizer {
		return err
	}

	// Assert finalizer is properly installed on the object
	if err := scopeFinalizer.AssertIsInstalled(ctx, r.Client, actualScope); err != nil {
		return err
	}

	// Scope already created auth server side
	if actualScope.IsCreatedOnAuthServer() {
		// Scope can have been manually deleted
		scope, err := r.API.ReadScope(ctx, actualScope.Status.AuthServerID)
		if err != nil && err != ErrNotFound {
			return err
		}
		if scope != nil { // If found, just check the label and update if required
			if scope.Label != actualScope.Spec.Label {
				return r.API.UpdateScope(ctx, scope.Id, actualScope.Spec.Label)
			}
			return nil
		}
		// Scope was deleted
		actualScope.ClearAuthServerID()
	}

	// As it could be the status update of the reconciliation which could have been fail
	// the scope can exist auth server side, so try to find it
	scopeByLabel, err := r.API.ReadScopeByLabel(ctx, actualScope.Spec.Label)
	if err != nil && err != ErrNotFound {
		return err
	}

	// If the scope is not found auth server side, we can create the scope
	if scopeByLabel == nil {
		scope, err := r.API.CreateScope(ctx, actualScope.Spec.Label)
		if err != nil {
			return err
		}
		actualScope.Status.AuthServerID = scope.Id
	} else {
		// Just reuse the scope
		actualScope.Status.AuthServerID = scopeByLabel.Id
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScopeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authcomponentsv1beta1.Scope{}).
		Complete(r)
}

func NewReconciler(c client.Client, scheme *runtime.Scheme, api ScopeAPI) *ScopeReconciler {
	return &ScopeReconciler{
		Client: c,
		Scheme: scheme,
		API:    api,
	}
}
