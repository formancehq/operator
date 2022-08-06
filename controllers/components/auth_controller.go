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

	"github.com/numary/formance-operator/pkg/envutil"
	"github.com/traefik/traefik/v2/pkg/config/dynamic"
	traefik "github.com/traefik/traefik/v2/pkg/provider/kubernetes/crd/traefik/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
)

const (
	defaultImage = "ghcr.io/numary/auth:latest"
)

// AuthReconciler reconciles a Auth object
type AuthReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=components.formance.com,resources=auths,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=components.formance.com,resources=auths/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=components.formance.com,resources=auths/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *AuthReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciliation")
	defer func() {
		logger.Info("Reconciliation terminated")
	}()

	actualServer := &componentsv1beta1.Auth{}
	if err := r.Get(ctx, req.NamespacedName, actualServer); err != nil {
		return ctrl.Result{}, err
	}

	image := actualServer.Spec.Image
	if image == "" {
		image = defaultImage
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: actualServer.Namespace,
			Name:      "auth", // TODO: Handle multiple instance
		},
	}

	//TODO: Set controller/owner reference
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		env := make([]corev1.EnvVar, 0)
		if actualServer.Spec.Monitoring != nil {
			env = append(env, actualServer.Spec.Monitoring.Env()...)
		}
		env = append(env,
			envutil.Env("POSTGRES_URI", actualServer.Spec.Postgres.URI()),
			envutil.Env("DELEGATED_CLIENT_SECRET", actualServer.Spec.DelegatedOIDCServer.ClientSecret),
			envutil.Env("DELEGATED_CLIENT_ID", actualServer.Spec.DelegatedOIDCServer.ClientID),
			envutil.Env("DELEGATED_ISSUER", actualServer.Spec.DelegatedOIDCServer.Issuer),
			envutil.Env("BASE_URL", actualServer.Spec.BaseURL),
			envutil.Env("SIGNING_KEY", actualServer.Spec.SigningKey),
		)
		if actualServer.Spec.DevMode {
			env = append(env,
				envutil.Env("DEBUG", "1"),
				envutil.Env("CAOS_OIDC_DEV", "1"),
			)
		}
		deployment.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "auth",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "auth",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "auth",
							Image: image,
							Command: []string{
								"/main",
								"serve",
							},
							Args:       nil,
							WorkingDir: "",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
								},
							},
							Env: env,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/_healthcheck",
										Port: intstr.IntOrString{
											IntVal: 8080,
										},
										Scheme: "HTTP",
									},
								},
								InitialDelaySeconds:           1,
								TimeoutSeconds:                30,
								PeriodSeconds:                 2,
								SuccessThreshold:              1,
								FailureThreshold:              10,
								TerminationGracePeriodSeconds: pointer.Int64(10),
							},
						},
					},
				},
			},
		}
		if err := controllerutil.SetControllerReference(actualServer, deployment, r.Scheme); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "auth",
			Namespace: actualServer.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Spec = corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Selector: deployment.Spec.Template.GetLabels(),
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	middleware := &traefik.Middleware{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: actualServer.Namespace,
			Name:      "auth-stripprefix",
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, middleware, func() error {
		middleware.Spec = traefik.MiddlewareSpec{
			StripPrefix: &dynamic.StripPrefix{
				Prefixes: []string{"/auth"},
			},
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: actualServer.Namespace,
			Name:      "auth",
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = map[string]string{
			"traefik.ingress.kubernetes.io/router.middlewares": actualServer.Namespace + "-" + middleware.Name + "@kubernetescrd",
		}
		ingress.Spec = networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "kubernetes.docker.internal", //TODO: Make configurable
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/auth",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: service.Name,
											Port: networkingv1.ServiceBackendPort{
												Name: service.Spec.Ports[0].Name,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *AuthReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&componentsv1beta1.Auth{}).
		Complete(r)
}

func NewAuthReconciler(client client.Client, scheme *runtime.Scheme) *AuthReconciler {
	return &AuthReconciler{
		Client: client,
		Scheme: scheme,
	}
}
