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

	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/typeutils"
	pkgError "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	autoscallingv1 "k8s.io/api/autoscaling/v1"
	autoscallingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Mutator reconciles a Auth object
type NextMutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=nexts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=nexts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=nexts/finalizers,verbs=update

func (r *NextMutator) Mutate(ctx context.Context, next *componentsv1beta2.Next) (*ctrl.Result, error) {

	apisv1beta1.SetProgressing(next)

	deployment, err := r.reconcileDeployment(ctx, next)
	if err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := r.reconcileService(ctx, next, deployment)
	if err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if next.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, next, service)
		if err != nil {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      next.Name,
				Namespace: next.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		apisv1beta1.RemoveIngressCondition(next)
	}

	if _, err := r.reconcileHPA(ctx, next); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling HPA")
	}

	apisv1beta1.SetReady(next)

	return nil, nil
}

func (r *NextMutator) reconcileDeployment(ctx context.Context, next *componentsv1beta2.Next) (*appsv1.Deployment, error) {
	matchLabels := CreateMap("app.kubernetes.io/name", "next")

	var env []corev1.EnvVar
	env = append(env, next.Spec.Postgres.Env("")...)
	env = append(env, next.Spec.DevProperties.EnvWithPrefix("")...)
	if next.Spec.Monitoring != nil {
		env = append(env, next.Spec.Monitoring.Env("")...)
	}

	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(next), next, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: next.Spec.GetReplicas(),
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "next",
						Image:           controllerutils.GetImage("next", next.Spec.Version),
						ImagePullPolicy: controllerutils.ImagePullPolicy(next.Spec),
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "next",
							ContainerPort: 8080,
						}},
						//LivenessProbe: &corev1.Probe{
						//	ProbeHandler: corev1.ProbeHandler{
						//		HTTPGet: &corev1.HTTPGetAction{
						//			Path: "/_health",
						//			Port: intstr.IntOrString{
						//				IntVal: 8080,
						//			},
						//			Scheme: "HTTP",
						//		},
						//	},
						//	InitialDelaySeconds:           1,
						//	TimeoutSeconds:                30,
						//	PeriodSeconds:                 2,
						//	SuccessThreshold:              1,
						//	FailureThreshold:              10,
						//	TerminationGracePeriodSeconds: pointer.Int64(10),
						//},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
								corev1.ResourceMemory: *resource.NewMilliQuantity(256, resource.DecimalSI),
							},
						},
					}},
				},
			},
		}
		if next.Spec.Postgres.CreateDatabase {
			deployment.Spec.Template.Spec.InitContainers = []corev1.Container{{
				Name:            "init-create-next-db",
				Image:           "postgres:13",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{
					"sh",
					"-c",
					`psql -Atx ${POSTGRES_URI}/postgres -c "SELECT 1 FROM pg_database WHERE datname = '${POSTGRES_DATABASE}'" | grep -q 1 && echo "Base already exists" || psql -Atx ${POSTGRES_URI}/postgres -c "CREATE DATABASE \"${POSTGRES_DATABASE}\""`,
				},
				Env: next.Spec.Postgres.Env(""),
			}}
		}
		return nil
	})
	switch {
	case err != nil:
		apisv1beta1.SetDeploymentError(next, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetDeploymentReady(next)
	}

	selector, err := metav1.LabelSelectorAsSelector(ret.Spec.Selector)
	if err != nil {
		return nil, err
	}

	next.Status.Selector = selector.String()
	next.Status.Replicas = *next.Spec.GetReplicas()

	return ret, err
}

func (r *NextMutator) reconcileHPA(ctx context.Context, next *componentsv1beta2.Next) (*autoscallingv2.HorizontalPodAutoscaler, error) {
	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(next), next, func(hpa *autoscallingv2.HorizontalPodAutoscaler) error {
		hpa.Spec = next.Spec.GetHPASpec(next)
		return nil
	})
	switch {
	case err != nil:
		apisv1beta1.SetHPAError(next, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetHPAReady(next)
	}
	return ret, err
}

func (r *NextMutator) reconcileService(ctx context.Context, next *componentsv1beta2.Next, deployment *appsv1.Deployment) (*corev1.Service, error) {
	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(next), next, func(service *corev1.Service) error {
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
		apisv1beta1.SetServiceError(next, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetServiceReady(next)
	}
	return ret, err
}

func (r *NextMutator) reconcileIngress(ctx context.Context, next *componentsv1beta2.Next, service *corev1.Service) (*networkingv1.Ingress, error) {
	annotations := next.Spec.Ingress.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	middlewareAuth := fmt.Sprintf("%s-auth-middleware@kubernetescrd", next.Namespace)
	annotations["traefik.ingress.kubernetes.io/router.middlewares"] = fmt.Sprintf("%s, %s", middlewareAuth, annotations["traefik.ingress.kubernetes.io/router.middlewares"])
	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(next), next, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: next.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
			Rules: []networkingv1.IngressRule{
				{
					Host: next.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     next.Spec.Ingress.Path,
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
		apisv1beta1.SetIngressError(next, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetIngressReady(next)
	}
	return ret, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NextMutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&autoscallingv1.HorizontalPodAutoscaler{})
	return nil
}

func NewNextMutator(client client.Client, scheme *runtime.Scheme) controllerutils.Mutator[*componentsv1beta2.Next] {
	return &NextMutator{
		Client: client,
		Scheme: scheme,
	}
}
