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
	"fmt"
	"strconv"
	"strings"

	authcomponentsv1beta2 "github.com/numary/operator/apis/auth.components/v1beta2"
	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/typeutils"
	pkgError "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// WalletsMutator reconciles a Auth object
type WalletsMutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=wallets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=wallets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=wallets/finalizers,verbs=update

func (r *WalletsMutator) Mutate(ctx context.Context, wallets *componentsv1beta2.Wallets) (*ctrl.Result, error) {

	apisv1beta1.SetProgressing(wallets)

	deployment, err := r.reconcileDeployment(ctx, wallets)
	if err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling deployment")
	}

	_, err = r.reconcileWorkersDeployment(ctx, wallets)
	if err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling workers deployment")
	}

	service, err := r.reconcileService(ctx, wallets, deployment)
	if err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if wallets.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, wallets, service)
		if err != nil {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      wallets.Name,
				Namespace: wallets.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		apisv1beta1.RemoveIngressCondition(wallets)
	}

	apisv1beta1.SetReady(wallets)

	return nil, nil
}

func envVars(wallets *componentsv1beta2.Wallets) []corev1.EnvVar {
	env := wallets.Spec.Postgres.Env("")
	env = append(env,
		apisv1beta1.Env("KAFKA_BROKERS", strings.Join(wallets.Spec.Collector.Brokers, ", ")),
		apisv1beta1.Env("KAFKA_TOPICS", wallets.Spec.Collector.Topic),
		apisv1beta1.Env("KAFKA_TLS_ENABLED", strconv.FormatBool(wallets.Spec.Collector.TLS)),
	)
	env = append(env, wallets.Spec.Postgres.Env("")...)
	if wallets.Spec.Collector.SASL != nil {
		env = append(env,
			apisv1beta1.Env("KAFKA_SASL_ENABLED", "true"),
			apisv1beta1.Env("KAFKA_SASL_MECHANISM", wallets.Spec.Collector.SASL.Mechanism),
			apisv1beta1.Env("KAFKA_USERNAME", wallets.Spec.Collector.SASL.Username),
			apisv1beta1.Env("KAFKA_PASSWORD", wallets.Spec.Collector.SASL.Password),
			apisv1beta1.Env("KAFKA_CONSUMER_GROUP", wallets.GetName()),
		)
	}

	env = append(env, wallets.Spec.DevProperties.Env()...)
	if wallets.Spec.Monitoring != nil {
		env = append(env, wallets.Spec.Monitoring.Env("")...)
	}
	return env
}

func (r *WalletsMutator) reconcileDeployment(ctx context.Context, wallets *componentsv1beta2.Wallets) (*appsv1.Deployment, error) {
	matchLabels := CreateMap("app.kubernetes.io/name", "wallets")

	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(wallets), wallets, func(deployment *appsv1.Deployment) error {
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
						Name:            "wallets",
						Image:           controllerutils.GetImage("wallets", wallets.Spec.Version),
						ImagePullPolicy: controllerutils.ImagePullPolicy(wallets.Spec),
						Env:             envVars(wallets),
						Ports: []corev1.ContainerPort{{
							Name:          "wallets",
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
		apisv1beta1.SetDeploymentError(wallets, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetDeploymentReady(wallets)
	}
	return ret, err
}

func (r *WalletsMutator) reconcileWorkersDeployment(ctx context.Context, wallets *componentsv1beta2.Wallets) (*appsv1.Deployment, error) {
	matchLabels := CreateMap("app.kubernetes.io/name", "wallets-workers")

	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, types.NamespacedName{
		Namespace: wallets.Namespace,
		Name:      wallets.Name + "-workers",
	}, wallets, func(deployment *appsv1.Deployment) error {
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
						Name:            "wallets-retries",
						Image:           controllerutils.GetImage("wallets", wallets.Spec.Version),
						ImagePullPolicy: controllerutils.ImagePullPolicy(wallets.Spec),
						Command:         []string{"wallets", "worker", "retries"},
						Env:             envVars(wallets),
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
						Name:            "wallets-messages",
						Image:           controllerutils.GetImage("wallets", wallets.Spec.Version),
						ImagePullPolicy: controllerutils.ImagePullPolicy(wallets.Spec),
						Command:         []string{"wallets", "worker", "messages"},
						Env:             envVars(wallets),
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
		apisv1beta1.SetDeploymentError(wallets, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetDeploymentReady(wallets)
	}
	return ret, err
}

func (r *WalletsMutator) reconcileService(ctx context.Context, auth *componentsv1beta2.Wallets, deployment *appsv1.Deployment) (*corev1.Service, error) {
	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(service *corev1.Service) error {
		service.Spec = corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:        "wallets",
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
		apisv1beta1.SetServiceError(auth, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetServiceReady(auth)
	}
	return ret, err
}

func (r *WalletsMutator) reconcileIngress(ctx context.Context, wallets *componentsv1beta2.Wallets, service *corev1.Service) (*networkingv1.Ingress, error) {
	annotations := wallets.Spec.Ingress.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	middlewareAuth := fmt.Sprintf("%s-auth-middleware@kubernetescrd", wallets.Namespace)
	annotations["traefik.ingress.kubernetes.io/router.middlewares"] = fmt.Sprintf("%s, %s", middlewareAuth, annotations["traefik.ingress.kubernetes.io/router.middlewares"])
	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(wallets), wallets, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: wallets.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
			Rules: []networkingv1.IngressRule{
				{
					Host: wallets.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     wallets.Spec.Ingress.Path,
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
		apisv1beta1.SetIngressError(wallets, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetIngressReady(wallets)
	}
	return ret, nil
}

// SetupWithBuilder SetupWithManager sets up the controller with the Manager.
func (r *WalletsMutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&authcomponentsv1beta2.Scope{})
	return nil
}

func NewWalletsMutator(client client.Client, scheme *runtime.Scheme) controllerutils.Mutator[*componentsv1beta2.Wallets] {
	return &WalletsMutator{
		Client: client,
		Scheme: scheme,
	}
}
