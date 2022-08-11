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
	"fmt"

	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	"github.com/numary/formance-operator/apis/stack/v1beta1"
	"github.com/numary/formance-operator/pkg/resourceutil"
	pkgError "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// StackReconciler reconciles a Stack object
type StackReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.stack.formance.com,resources=auths,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks/finalizers,verbs=update

func (r *StackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Starting Stack reconciliation")

	logger.Info("Add status for Stack is Pending")
	actualStack := &v1beta1.Stack{}
	if err := r.Get(ctx, req.NamespacedName, actualStack); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Skip reconcile because of not found")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, pkgError.Wrap(err, "Reading stack")
	}

	result, reconcileError := r.reconcile(ctx, actualStack)
	if reconcileError != nil {
		log.FromContext(ctx).Error(reconcileError, "Reconciling")
	}
	if patchErr := r.Status().Update(ctx, actualStack); patchErr != nil {
		return ctrl.Result{}, patchErr
	}

	if result != nil {
		return *result, nil
	}

	return ctrl.Result{
		Requeue: reconcileError != nil,
	}, nil

	// Get Actual Stack Status
	//actualStack := &v1beta1.Stack{}
	//if err := r.Get(ctx, req.NamespacedName, actualStack); err != nil {
	//	if errors.IsNotFound(err) {
	//		logger.Info("Stack resource not found")
	//		return ctrl.Result{}, nil
	//	}
	//	logger.Info("Failed to fetch Stack resource")
	//	return ctrl.Result{}, err
	//}
	//actualStack = actualStack.DeepCopy()
	//
	//// Generate Annotations for Stack
	//annotations := make(map[string]string)
	//annotations["stack.formance.com/name"] = actualStack.Name
	//annotations["stack.formance.com/version"] = actualStack.Spec.Version
	//
	//labels := make(map[string]string)
	//labels["stack.formance.com/name"] = actualStack.Name
	//labels["stack.formance.com/version"] = actualStack.Spec.Version
	//
	//// Create Config Object
	//config := Config{
	//	Context:     ctx,
	//	Request:     req,
	//	Stack:       *actualStack,
	//	Annotations: annotations,
	//	Labels:      labels,
	//}
	//
	//// Add Reconcile for Ledger
	//r.reconcileLedger(config)
}

func (r *StackReconciler) reconcile(ctx context.Context, actual *v1beta1.Stack) (*ctrl.Result, error) {
	actual.Progress()

	if err := r.reconcileNamespace(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling namespace")
	}
	if err := r.reconcileAuth(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling Auth")
	}

	actual.SetReady()
	return nil, nil
}

//func (r *StackReconciler) reconcileLedger(ctx context.Context, actualStack *v1beta1.Stack) (ctrl.Result, error) {
//	logger := log.FromContext(ctx, "Ledger", actualStack)
//	logger.Info("Starting Ledger reconciliation")
//	// Update value in Config object
//	config.Stack.Name = config.Stack.Name + "-ledger"
//	config.Labels["stack.formance.com/component"] = "ledger"
//
//	// Namespace Reconcile
//	r.reconcileNamespace(logger, config)
//	// Service Reconcile
//	r.reconcileService(logger, config)
//	// Ingress Reconcile
//	r.reconcileIngress(logger, config)
//
//	return ctrl.Result{}, nil
//}

func (r *StackReconciler) reconcileNamespace(ctx context.Context, stack *v1beta1.Stack) error {
	log.FromContext(ctx).Info("Reconciling Namespace")

	_, operationResult, err := resourceutil.CreateOrUpdateWithOwner(ctx, r.Client, r.Scheme, types.NamespacedName{
		Name: stack.Spec.Namespace,
	}, stack, func(ns *corev1.Namespace) error {
		// No additional mutate needed
		return nil
	})
	switch {
	case err != nil:
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetNamespaceCreated()
	}

	log.FromContext(ctx).Info("Namespace ready")
	return nil
}

func (r *StackReconciler) reconcileAuth(ctx context.Context, stack *v1beta1.Stack) error {
	log.FromContext(ctx).Info("Reconciling Auth")

	if stack.Spec.Auth == nil {
		err := r.Client.Delete(ctx, &authcomponentsv1beta1.Auth{
			ObjectMeta: v1.ObjectMeta{
				Namespace: stack.Spec.Namespace,
				Name:      stack.Spec.Auth.Name(stack),
			},
		})
		switch {
		case errors.IsNotFound(err):
		case err != nil:
			return pkgError.Wrap(err, "Deleting Auth")
		default:
			stack.RemoveAuthStatus()
		}
		return nil
	}

	_, operationResult, err := resourceutil.CreateOrUpdateWithOwner(ctx, r.Client, r.Scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.Spec.Auth.Name(stack),
	}, stack, func(ns *authcomponentsv1beta1.Auth) error {
		var ingress *authcomponentsv1beta1.IngressSpec
		if stack.Spec.Ingress != nil {
			ingress = &authcomponentsv1beta1.IngressSpec{
				Path:        "/auth",
				Host:        stack.Spec.Host,
				Annotations: stack.Spec.Ingress.Annotations,
			}
		}
		ns.Spec = authcomponentsv1beta1.AuthSpec{
			Image:               stack.Spec.Auth.Image,
			Postgres:            stack.Spec.Auth.PostgresConfig,
			BaseURL:             fmt.Sprintf("%s://%s/auth", stack.Scheme(), stack.Spec.Host),
			SigningKey:          stack.Spec.Auth.SigningKey,
			DevMode:             stack.Spec.Debug,
			Ingress:             ingress,
			DelegatedOIDCServer: stack.Spec.Auth.DelegatedOIDCServer,
			Monitoring:          stack.Spec.Monitoring,
		}
		return nil
	})
	switch {
	case err != nil:
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetAuthCreated()
	}

	log.FromContext(ctx).Info("Auth ready")
	return nil
}

//func (r *StackReconciler) reconcileService(logger logr.Logger, config Config) (ctrl.Result, error) {
//	logger.Info("Reconciling Service")
//
//	deploy := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: config.Stack.Name, Namespace: config.Stack.Spec.Namespace, Annotations: config.Annotations}}
//
//	_, err := controllerutil.CreateOrUpdate(config.Context, r.Client, deploy, func() error {
//		deploy.Spec.Ports = []corev1.ServicePort{{Port: int32(config.Stack.Spec.Services.Ledger.Port)}}
//		deploy.Spec.Selector = config.Labels
//		return nil
//	})
//
//	if err != nil {
//		logger.Error(err, "Service reconcile failed")
//		return ctrl.Result{}, err
//	}
//
//	logger.Info("Service ready")
//	return ctrl.Result{}, nil
//}

//func (r *StackReconciler) reconcileIngress(logger logr.Logger, config Config) (ctrl.Result, error) {
//	logger.Info("Reconciling Ingress")
//
//	deploy := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: config.Stack.Name, Namespace: config.Stack.Spec.Namespace, Annotations: config.Annotations}}
//
//	_, err := controllerutil.CreateOrUpdate(config.Context, r.Client, deploy, func() error {
//		traefikIngressClassName := "formance-gateway"
//		ingressPathType := networkingv1.PathTypePrefix
//		deploy.Spec.IngressClassName = &traefikIngressClassName
//		deploy.Spec.Rules = []networkingv1.IngressRule{
//			{
//				Host: config.Stack.Spec.Url,
//				IngressRuleValue: networkingv1.IngressRuleValue{
//					HTTP: &networkingv1.HTTPIngressRuleValue{
//						Paths: []networkingv1.HTTPIngressPath{
//							{
//								Path:     "/api/ledger",
//								PathType: &ingressPathType,
//								Backend: networkingv1.IngressBackend{
//									Service: &networkingv1.IngressServiceBackend{
//										Name: config.Stack.Name,
//										Port: networkingv1.ServiceBackendPort{
//											Number: int32(config.Stack.Spec.Services.Ledger.Port),
//										},
//									},
//									Resource: nil,
//								},
//							},
//						},
//					},
//				},
//			},
//		}
//		return nil
//	})
//
//	if err != nil {
//		logger.Error(err, "Ingress reconcile failed")
//		return ctrl.Result{}, err
//	}
//
//	logger.Info("Ingress ready")
//	return ctrl.Result{}, nil
//}

// SetupWithManager sets up the controller with the Manager.
func (r *StackReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.Stack{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		//Owns(&authcomponentsv1beta1.Ledger{}).
		//Owns(&authcomponentsv1beta1.Control{}).
		//Owns(&authcomponentsv1beta1.Payments{}).
		//Owns(&authcomponentsv1beta1.Search{}).
		Owns(&authcomponentsv1beta1.Auth{}).
		Owns(&corev1.Namespace{}).
		Complete(r)
}

func NewReconciler(client client.Client, scheme *runtime.Scheme) *StackReconciler {
	return &StackReconciler{
		Client: client,
		Scheme: scheme,
	}
}
