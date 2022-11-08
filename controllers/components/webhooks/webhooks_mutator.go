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
	"fmt"

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

func (r *Mutator) Mutate(ctx context.Context, webhooks *componentsv1beta1.Webhooks) (*ctrl.Result, error) {

	SetProgressing(webhooks)

	deployment, err := r.reconcileDeployment(ctx, webhooks)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling deployment")
	}

	_, err = r.reconcileWorkersDeployment(ctx, webhooks)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling workers deployment")
	}

	service, err := r.reconcileService(ctx, webhooks, deployment)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if webhooks.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, webhooks, service)
		if err != nil {
			return Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      webhooks.Name,
				Namespace: webhooks.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(webhooks)
	}

	SetReady(webhooks)

	return nil, nil
}

func (r *Mutator) reconcileDeployment(ctx context.Context, webhooks *componentsv1beta1.Webhooks) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.CreateMap("app.kubernetes.io/name", "webhooks")

	env := webhooks.Spec.MongoDB.Env("")
	env = append(env, EnvWithPrefix("", "STORAGE_MONGO_CONN_STRING", "$(MONGODB_URI)"))
	env = append(env, EnvWithPrefix("", "STORAGE_MONGO_DATABASE_NAME", ComputeEnvVar("", "$(MONGODB_DATABASE)")))
	env = append(env, EnvWithPrefix("", "KAFKA_BROKERS", ComputeEnvVar("", "$(PUBLISHER_KAFKA_BROKER)")))
	env = append(env, EnvWithPrefix("", "KAFKA_TOPICS", ComputeEnvVar("", "$(PUBLISHER_TOPIC_MAPPING)")))
	env = append(env, EnvWithPrefix("", "KAFKA_TLS_ENABLED", ComputeEnvVar("", "$(PUBLISHER_KAFKA_TLS_ENABLED)")))
	env = append(env, EnvWithPrefix("", "KAFKA_SASL_ENABLED", ComputeEnvVar("", "$(PUBLISHER_KAFKA_SASL_ENABLED)")))
	env = append(env, EnvWithPrefix("", "KAFKA_SASL_MECHANISM", ComputeEnvVar("", "$(PUBLISHER_KAFKA_SASL_MECHANISM)")))
	env = append(env, EnvWithPrefix("", "KAFKA_USERNAME", ComputeEnvVar("", "$(PUBLISHER_KAFKA_SASL_USERNAME)")))
	env = append(env, EnvWithPrefix("", "KAFKA_PASSWORD", webhooks.Spec.Collector.Topic))

	if webhooks.Spec.Debug {
		env = append(env, Env("DEBUG", "true"))
	}
	if webhooks.Spec.Auth != nil {
		env = append(env, webhooks.Spec.Auth.Env("")...)
	}
	if webhooks.Spec.Monitoring != nil {
		env = append(env, webhooks.Spec.Monitoring.Env("")...)
	}
	if webhooks.Spec.Collector != nil {
		env = append(env, webhooks.Spec.Collector.Env("")...)
	}

	image := webhooks.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(webhooks), webhooks, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: webhooks.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Name:            "webhooks",
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
					}},
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		SetDeploymentError(webhooks, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(webhooks)
	}
	return ret, err
}

func (r *Mutator) reconcileWorkersDeployment(ctx context.Context, webhooks *componentsv1beta1.Webhooks) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.CreateMap("app.kubernetes.io/name", "webhooks-workers")
	webhooks.Name = fmt.Sprintf("%s-webhooks-worker", webhooks.Namespace)

	env := webhooks.Spec.MongoDB.Env("")
	env = append(env, EnvWithPrefix("", "STORAGE_MONGO_CONN_STRING", "$(MONGODB_URI)"))
	env = append(env, EnvWithPrefix("", "STORAGE_MONGO_DATABASE_NAME", ComputeEnvVar("", "$(MONGODB_DATABASE)")))
	env = append(env, EnvWithPrefix("", "KAFKA_BROKERS", ComputeEnvVar("", "$(PUBLISHER_KAFKA_BROKER)")))
	env = append(env, EnvWithPrefix("", "KAFKA_TOPICS", ComputeEnvVar("", "$(PUBLISHER_TOPIC_MAPPING)")))
	env = append(env, EnvWithPrefix("", "KAFKA_TLS_ENABLED", ComputeEnvVar("", "$(PUBLISHER_KAFKA_TLS_ENABLED)")))
	env = append(env, EnvWithPrefix("", "KAFKA_SASL_ENABLED", ComputeEnvVar("", "$(PUBLISHER_KAFKA_SASL_ENABLED)")))
	env = append(env, EnvWithPrefix("", "KAFKA_SASL_MECHANISM", ComputeEnvVar("", "$(PUBLISHER_KAFKA_SASL_MECHANISM)")))
	env = append(env, EnvWithPrefix("", "KAFKA_USERNAME", ComputeEnvVar("", "$(PUBLISHER_KAFKA_SASL_USERNAME)")))
	env = append(env, EnvWithPrefix("", "KAFKA_PASSWORD", webhooks.Spec.Collector.Topic))

	if webhooks.Spec.Debug {
		env = append(env, Env("DEBUG", "true"))
	}
	if webhooks.Spec.Auth != nil {
		env = append(env, webhooks.Spec.Auth.Env("")...)
	}
	if webhooks.Spec.Monitoring != nil {
		env = append(env, webhooks.Spec.Monitoring.Env("")...)
	}
	if webhooks.Spec.Collector != nil {
		env = append(env, webhooks.Spec.Collector.Env("")...)
	}

	image := webhooks.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(webhooks), webhooks, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: webhooks.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Name:            "webhooks-retries",
						Image:           image,
						ImagePullPolicy: ImagePullPolicy(image),
						Command:         []string{"workers", "retries"},
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "retries",
							ContainerPort: 8082,
						}},
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/_healthcheck",
									Port: intstr.IntOrString{
										IntVal: 8082,
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
					}, {
						Name:            "webhooks-messages",
						Image:           image,
						ImagePullPolicy: ImagePullPolicy(image),
						Command:         []string{"worker", "messages"},
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "messages",
							ContainerPort: 8081,
						}},
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/_healthcheck",
									Port: intstr.IntOrString{
										IntVal: 8081,
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
		SetDeploymentError(webhooks, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(webhooks)
	}
	return ret, err
}

func (r *Mutator) reconcileService(ctx context.Context, auth *componentsv1beta1.Webhooks, deployment *appsv1.Deployment) (*corev1.Service, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(service *corev1.Service) error {
		service.Spec = corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:        "webhooks",
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

func (r *Mutator) reconcileIngress(ctx context.Context, webhooks *componentsv1beta1.Webhooks, service *corev1.Service) (*networkingv1.Ingress, error) {
	annotations := webhooks.Spec.Ingress.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	middlewareAuth := fmt.Sprintf("%s-auth-middleware@kubernetescrd", webhooks.Namespace)
	annotations["traefik.ingress.kubernetes.io/router.middlewares"] = fmt.Sprintf("%s, %s", middlewareAuth, annotations["traefik.ingress.kubernetes.io/router.middlewares"])
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(webhooks), webhooks, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: webhooks.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
			Rules: []networkingv1.IngressRule{
				{
					Host: webhooks.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     webhooks.Spec.Ingress.Path,
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
		SetIngressError(webhooks, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetIngressReady(webhooks)
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
