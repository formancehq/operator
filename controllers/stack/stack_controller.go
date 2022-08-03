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

package stack

import (
	"context"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/numary/formance-operator/apis/stack/v1beta1"
)

//+kubebuilder:rbac:groups=stack.formance.com,resources=stacks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=stack.formance.com,resources=stacks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=stack.formance.com,resources=stacks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Stack object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *StackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Starting Stack reconciliation")

	logger.Info("Add status for Stack is Pending")
	actual := &v1beta1.Stack{}
	actual.Status.Progress = v1beta1.StackProgressPending
	actual.Status.SetReady()

	// Get Actual Stack Status
	actualStack := &v1beta1.Stack{}
	if err := r.Get(ctx, req.NamespacedName, actualStack); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Stack resource not found")
			return ctrl.Result{}, nil
		}
		logger.Info("Failed to fetch Stack resource")
		return ctrl.Result{}, err
	}
	actualStack = actualStack.DeepCopy()

	// Generate Annotations for Stack
	annotations := make(map[string]string)
	annotations["stack.formance.com/name"] = actualStack.Name
	annotations["stack.formance.com/version"] = actualStack.Spec.Version

	labels := make(map[string]string)
	labels["stack.formance.com/name"] = actualStack.Name
	labels["stack.formance.com/version"] = actualStack.Spec.Version

	// Create Config Object
	config := Config{
		Context:     ctx,
		Request:     req,
		Stack:       *actualStack,
		Annotations: annotations,
		Labels:      labels,
	}

	// Add Reconcile for Ledger
	r.NewLedgerReconcile(config)

	return ctrl.Result{}, nil
}

func (r *StackReconciler) NewLedgerReconcile(config Config) (ctrl.Result, error) {
	logger := log.FromContext(config.Context, "Ledger", config.Request.NamespacedName)
	logger.Info("Starting Ledger reconciliation")
	// Update value in Config object
	config.Stack.Name = config.Stack.Name + "-ledger"
	config.Labels["stack.formance.com/component"] = "ledger"

	// Namespace Reconcile
	r.reconcileNamespace(logger, config)
	// Service Reconcile
	r.reconcileService(logger, config)
	// Ingress Reconcile
	r.reconcileIngress(logger, config)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StackReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.Stack{}).
		Complete(r)
}
