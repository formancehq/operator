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

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var stackLog = logf.Log.WithName("stack-resource")

func (r *Stack) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/validate-stack-formance-com-v1beta1-stack,mutating=false,failurePolicy=fail,sideEffects=None,groups=stack.formance.com,resources=stacks,verbs=create;update;delete,versions=v1beta1,name=vstack.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &Stack{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Stack) ValidateCreate() error {
	stackLog.Info("validate create", "name", r.Name)
	errs := r.Spec.Validate()
	if len(errs) == 0 {
		return nil
	}
	return errors.NewInvalid(
		schema.GroupKind{Group: GroupVersion.Group, Kind: "Stack"},
		r.Name, errs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Stack) ValidateUpdate(old runtime.Object) error {
	stackLog.Info("validate update", "name", r.Name)
	return r.ValidateCreate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Stack) ValidateDelete() error {
	stackLog.Info("validate delete", "name", r.Name)
	return nil
}
