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

package components

import (
	"context"

	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PaymentsReconciler reconciles a Payments object
type PaymentsReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=components.formance.com,resources=payments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=components.formance.com,resources=payments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=components.formance.com,resources=payments/finalizers,verbs=update

func (r *PaymentsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PaymentsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&componentsv1beta1.Payments{}).
		Complete(r)
}
