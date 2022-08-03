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
package authcomponents

import (
	"context"

	"github.com/numary/auth/authclient"
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/auth.components/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ScopeReconciler reconciles a Scope object
type ScopeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	API    AuthServerAPI
}

func isDeleted(meta client.Object) bool {
	return meta.GetDeletionTimestamp() == nil || meta.GetDeletionTimestamp().IsZero()
}

var scopeFinalizer = newFinalizer("auth.components.formance.com/finalizer")

//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=auth.components.formance.com,resources=scopes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *ScopeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, r.reconcile(ctx, req)
}

func (r *ScopeReconciler) reconcile(ctx context.Context, req ctrl.Request) error {
	actualScope := &authcomponentsv1beta1.Scope{}
	if err := r.Get(ctx, req.NamespacedName, actualScope); err != nil {
		return err
	}

	if isDeleted(actualScope) {
		if !scopeFinalizer.isPresent(actualScope) {
			return nil
		}
		if actualScope.IsCreatedOnAuthServer() {
			if err := r.API.DeleteScope(ctx, actualScope.Status.AuthServerID); err != nil {
				return err
			}
		}
		if err := scopeFinalizer.removeFinalizer(ctx, r.Client, actualScope); err != nil {
			return err
		}
		return nil
	}

	if !scopeFinalizer.isPresent(actualScope) {
		if err := scopeFinalizer.add(ctx, r.Client, actualScope); err != nil {
			return err
		}
	}

	if actualScope.Status.AuthServerID != "" {
		allScopes, err := r.API.ListScopes(ctx)
		if err != nil {
			return err
		}

		if scope := allScopes.First(func(scope authclient.Scope) bool {
			return scope.Label == actualScope.Spec.Label
		}); scope != nil {
			if scope.Label != actualScope.Spec.Label {
				return r.API.UpdateScope(ctx, scope.Id, actualScope.Spec.Label)
			}
			return nil
		}
		actualScope.Status.AuthServerID = ""
	}

	id, err := r.API.CreateScope(ctx, actualScope.Spec.Label)
	if err != nil {
		return err
	}

	actualScope.Status.AuthServerID = id
	if err := r.Client.Update(ctx, actualScope); err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScopeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authcomponentsv1beta1.Scope{}).
		Complete(r)
}
