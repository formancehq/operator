package stack

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *StackReconciler) reconcileNamespace(logger logr.Logger, config Config) (ctrl.Result, error) {
	logger.Info("Reconciling Namespace")

	deploy := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: config.Stack.Spec.Namespace, Annotations: config.Annotations}}

	_, err := controllerutil.CreateOrUpdate(config.Context, r.Client, deploy, func() error {
		return nil
	})

	if err != nil {
		logger.Error(err, "Namespace reconcile failed")
		return ctrl.Result{}, err
	}

	logger.Info("Namespace ready")
	return ctrl.Result{}, nil
}

func (r *StackReconciler) reconcileService(logger logr.Logger, config Config) (ctrl.Result, error) {
	logger.Info("Reconciling Service")

	deploy := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: config.Stack.Name, Namespace: config.Stack.Spec.Namespace, Annotations: config.Annotations}}

	_, err := controllerutil.CreateOrUpdate(config.Context, r.Client, deploy, func() error {
		deploy.Spec.Ports = []corev1.ServicePort{{Port: int32(config.Stack.Spec.Services.Ledger.Port)}}
		deploy.Spec.Selector = config.Labels
		return nil
	})

	if err != nil {
		logger.Error(err, "Service reconcile failed")
		return ctrl.Result{}, err
	}

	logger.Info("Service ready")
	return ctrl.Result{}, nil
}

func (r *StackReconciler) reconcileIngress(logger logr.Logger, config Config) (ctrl.Result, error) {
	logger.Info("Reconciling Ingress")

	deploy := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: config.Stack.Name, Namespace: config.Stack.Spec.Namespace, Annotations: config.Annotations}}

	_, err := controllerutil.CreateOrUpdate(config.Context, r.Client, deploy, func() error {
		traefikIngressClassName := "formance-gateway"
		ingressPathType := networkingv1.PathTypePrefix
		deploy.Spec.IngressClassName = &traefikIngressClassName
		deploy.Spec.Rules = []networkingv1.IngressRule{
			{
				Host: config.Stack.Spec.Url,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/api/ledger",
								PathType: &ingressPathType,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: config.Stack.Name,
										Port: networkingv1.ServiceBackendPort{
											Number: int32(config.Stack.Spec.Services.Ledger.Port),
										},
									},
									Resource: nil,
								},
							},
						},
					},
				},
			},
		}
		return nil
	})

	if err != nil {
		logger.Error(err, "Ingress reconcile failed")
		return ctrl.Result{}, err
	}

	logger.Info("Ingress ready")
	return ctrl.Result{}, nil
}
