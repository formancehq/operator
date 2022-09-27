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

package webhooks

import (
	"context"
	authcomponentsv1beta1 "github.com/numary/operator/apis/components/auth/v1beta1"
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	"github.com/numary/operator/internal"
	"github.com/numary/operator/internal/collectionutil"
	"github.com/numary/operator/internal/resourceutil"
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
	defaultImage = "ghcr.io/formancehq/webhooks:latest"
)

// Mutator reconciles a Auth object
type Mutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=webhooks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=webhooks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=webhooks/finalizers,verbs=update

func (r *Mutator) Mutate(ctx context.Context, webhook *componentsv1beta1.Webhooks) (*ctrl.Result, error) {

	SetProgressing(webhook)

	deployment, err := r.reconcileDeployment(ctx, webhook)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := r.reconcileService(ctx, webhook, deployment)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if webhook.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, webhook, service)
		if err != nil {
			return Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      webhook.Name,
				Namespace: webhook.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(webhook)
	}

	SetReady(webhook)

	return nil, nil
}

func (r *Mutator) reconcileDeployment(ctx context.Context, webhook *componentsv1beta1.Webhooks) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.CreateMap("app.kubernetes.io/name", "webhook")

	env := webhook.Spec.MongoDB.Env("")
	if webhook.Spec.Debug {
		env = append(env, Env("DEBUG", "true"))
	}
	if webhook.Spec.Auth != nil {
		env = append(env, webhook.Spec.Auth.Env("")...)
	}
	if webhook.Spec.Monitoring != nil {
		env = append(env, webhook.Spec.Monitoring.Env("")...)
	}
	if webhook.Spec.Collector != nil {
		env = append(env, webhook.Spec.Collector.Env("")...)
	}

	image := webhook.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(webhook), webhook, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: webhook.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Name:            "webhook",
						Image:           image,
						ImagePullPolicy: ImagePullPolicy(image),
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "webhooks",
							ContainerPort: 8080,
						}},
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/_health",
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
					}},
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		SetDeploymentError(webhook, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(webhook)
	}
	return ret, err
}

func (r *Mutator) reconcileService(ctx context.Context, auth *componentsv1beta1.Webhooks, deployment *appsv1.Deployment) (*corev1.Service, error) {
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
		SetServiceError(auth, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetServiceReady(auth)
	}
	return ret, err
}

func (r *Mutator) reconcileIngress(ctx context.Context, webhook *componentsv1beta1.Webhooks, service *corev1.Service) (*networkingv1.Ingress, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(webhook), webhook, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = webhook.Spec.Ingress.Annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: webhook.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
			Rules: []networkingv1.IngressRule{
				{
					Host: webhook.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     webhook.Spec.Ingress.Path,
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
		SetIngressError(webhook, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetIngressReady(webhook)
	}
	return ret, nil
}

// SetupWithBuilder SetupWithManager sets up the controller with the Manager.
func (r *Mutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&authcomponentsv1beta1.Scope{})
	return nil
}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*componentsv1beta1.Webhooks] {
	return &Mutator{
		Client: client,
		Scheme: scheme,
	}
}
