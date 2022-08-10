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

package auth

import (
	"context"

	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	"github.com/numary/formance-operator/internal"
	"github.com/numary/formance-operator/internal/collectionutil"
	"github.com/numary/formance-operator/pkg/containerutil"
	"github.com/numary/formance-operator/pkg/envutil"
	"github.com/numary/formance-operator/pkg/probeutil"
	"github.com/numary/formance-operator/pkg/resourceutil"
	pkgError "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	defaultImage = "ghcr.io/numary/auth:latest"
)

// Mutator reconciles a Auth object
type Mutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=auths,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=auths/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=auths/finalizers,verbs=update

func (r *Mutator) Mutate(ctx context.Context, auth *componentsv1beta1.Auth) (*ctrl.Result, error) {
	deployment, err := r.reconcileDeployment(ctx, auth)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := r.reconcileService(ctx, auth, deployment)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling service")
	}

	if auth.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, auth, service)
		if err != nil {
			return nil, pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      auth.Name,
				Namespace: auth.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return nil, pkgError.Wrap(err, "Deleting ingress")
		}
		auth.RemoveIngressStatus()
	}

	auth.SetReady()

	if err := r.Client.Status().Update(ctx, auth); err != nil {
		return nil, pkgError.Wrap(err, "Updating status")
	}

	return nil, nil
}

func (r *Mutator) reconcileDeployment(ctx context.Context, auth *componentsv1beta1.Auth) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.Create("app.kubernetes.io/name", "auth")
	port := int32(8080)

	env := []corev1.EnvVar{
		envutil.Env("POSTGRES_URI", auth.Spec.Postgres.URI()),
		envutil.Env("DELEGATED_CLIENT_SECRET", auth.Spec.DelegatedOIDCServer.ClientSecret),
		envutil.Env("DELEGATED_CLIENT_ID", auth.Spec.DelegatedOIDCServer.ClientID),
		envutil.Env("DELEGATED_ISSUER", auth.Spec.DelegatedOIDCServer.Issuer),
		envutil.Env("BASE_URL", auth.Spec.BaseURL),
		envutil.Env("SIGNING_KEY", auth.Spec.SigningKey),
	}
	if auth.Spec.DevMode {
		env = append(env,
			envutil.Env("DEBUG", "1"),
			envutil.Env("CAOS_OIDC_DEV", "1"),
		)
	}
	if auth.Spec.Monitoring != nil {
		env = append(env, auth.Spec.Monitoring.Env()...)
	}

	image := auth.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:          "auth",
						Image:         image,
						Command:       []string{"/main", "serve"},
						Ports:         containerutil.SinglePort("http", port),
						Env:           env,
						LivenessProbe: probeutil.DefaultLiveness(),
					}},
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		auth.SetDeploymentFailure(err)
	case operationResult == controllerutil.OperationResultNone:
	default:
		auth.SetDeploymentCreated()
	}
	return ret, err
}

func (r *Mutator) reconcileService(ctx context.Context, auth *componentsv1beta1.Auth, deployment *appsv1.Deployment) (*corev1.Service, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(service *corev1.Service) error {
		service.Spec = corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:        "http",
				Port:        deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort,
				Protocol:    "TCP",
				AppProtocol: pointer.String("http"),
				TargetPort:  intstr.FromString(deployment.Spec.Template.Spec.Containers[0].Ports[0].Name),
			}},
			Selector: deployment.Spec.Template.Labels,
		}
		return nil
	})
	switch {
	case err != nil:
		auth.SetServiceFailure(err)
	case operationResult == controllerutil.OperationResultNone:
	default:
		auth.SetServiceCreated()
	}
	return ret, err
}

func (r *Mutator) reconcileIngress(ctx context.Context, auth *componentsv1beta1.Auth, service *corev1.Service) (*networkingv1.Ingress, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = auth.Spec.Ingress.Annotations
		ingress.Spec = networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: auth.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     auth.Spec.Ingress.Path,
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
	})
	switch {
	case err != nil:
		auth.SetIngressFailure(err)
	case operationResult == controllerutil.OperationResultNone:
	default:
		auth.SetIngressCreated()
	}
	return ret, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Mutator) SetupWithBuilder(builder *ctrl.Builder) {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{})
}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[
	componentsv1beta1.AuthCondition, *componentsv1beta1.Auth] {
	return &Mutator{
		Client: client,
		Scheme: scheme,
	}
}