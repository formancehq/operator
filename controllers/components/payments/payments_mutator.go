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

package payments

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	authcomponentsv1beta1 "github.com/formancehq/operator/apis/components/auth/v1beta1"
	componentsv1beta1 "github.com/formancehq/operator/apis/components/v1beta1"
	. "github.com/formancehq/operator/apis/sharedtypes"
	"github.com/formancehq/operator/internal"
	"github.com/formancehq/operator/internal/collectionutil"
	"github.com/formancehq/operator/internal/resourceutil"
	pkgError "github.com/pkg/errors"
	"gopkg.in/yaml.v3"
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

//go:embed benthos-config.yml
var benthosConfigDir embed.FS

const (
	defaultImage = "ghcr.io/formancehq/payments:latest"
)

// Mutator reconciles a Auth object
type Mutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=payments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=payments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=payments/finalizers,verbs=update

func (r *Mutator) Mutate(ctx context.Context, payments *componentsv1beta1.Payments) (*ctrl.Result, error) {

	SetProgressing(payments)

	deployment, err := r.reconcileDeployment(ctx, payments)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := r.reconcileService(ctx, payments, deployment)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if err := r.reconcileIngestionStream(ctx, payments); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if payments.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, payments, service)
		if err != nil {
			return Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      payments.Name,
				Namespace: payments.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(payments)
	}

	SetReady(payments)

	return nil, nil
}

func (r *Mutator) reconcileDeployment(ctx context.Context, payments *componentsv1beta1.Payments) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.CreateMap("app.kubernetes.io/name", "payments")

	env := payments.Spec.MongoDB.Env("")
	if payments.Spec.Debug {
		env = append(env, Env("DEBUG", "true"))
	}
	if payments.Spec.Auth != nil {
		env = append(env, payments.Spec.Auth.Env("")...)
	}
	if payments.Spec.Monitoring != nil {
		env = append(env, payments.Spec.Monitoring.Env("")...)
	}
	if payments.Spec.Collector != nil {
		env = append(env, payments.Spec.Collector.Env("")...)
	}

	image := payments.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(payments), payments, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: payments.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Name:            "payments",
						Image:           image,
						ImagePullPolicy: ImagePullPolicy(image),
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "payments",
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
		SetDeploymentError(payments, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(payments)
	}
	return ret, err
}

func (r *Mutator) reconcileService(ctx context.Context, auth *componentsv1beta1.Payments, deployment *appsv1.Deployment) (*corev1.Service, error) {
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

func (r *Mutator) reconcileIngress(ctx context.Context, payments *componentsv1beta1.Payments, service *corev1.Service) (*networkingv1.Ingress, error) {
	annotations := payments.Spec.Ingress.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	middlewareAuth := fmt.Sprintf("%s-auth-middleware@kubernetescrd", payments.Namespace)
	annotations["traefik.ingress.kubernetes.io/router.middlewares"] = fmt.Sprintf("%s, %s", middlewareAuth, annotations["traefik.ingress.kubernetes.io/router.middlewares"])
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(payments), payments, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: payments.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
			Rules: []networkingv1.IngressRule{
				{
					Host: payments.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     payments.Spec.Ingress.Path,
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
		SetIngressError(payments, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetIngressReady(payments)
	}
	return ret, nil
}

func (r *Mutator) reconcileIngestionStream(ctx context.Context, payments *componentsv1beta1.Payments) error {
	_, ret, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, types.NamespacedName{
		Namespace: payments.Namespace,
		Name:      payments.Name + "-ingestion-stream",
	}, payments, func(t *componentsv1beta1.SearchIngester) error {
		data, err := benthosConfigDir.ReadFile("benthos-config.yml")
		if err != nil {
			return err
		}

		stream := map[string]any{}
		if err := yaml.Unmarshal(data, &stream); err != nil {
			return err
		}

		input := stream["input"].(map[string]any)
		eventBusInput := input["event_bus"].(map[string]any)
		eventBusInput["topic"] = fmt.Sprintf(payments.Name)
		eventBusInput["consumer_group"] = payments.Name

		data, err = json.Marshal(stream)
		if err != nil {
			return err
		}

		t.Spec.Pipeline = data
		t.Spec.Reference = fmt.Sprintf("%s-search", payments.Namespace)
		return nil
	})
	switch {
	case err != nil:
		SetCondition(payments, "IngestionStreamReady", metav1.ConditionFalse, err.Error())
		return err
	case ret == controllerutil.OperationResultNone:
	default:
		SetCondition(payments, "IngestionStreamReady", metav1.ConditionTrue)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Mutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&authcomponentsv1beta1.Scope{}).
		Owns(&componentsv1beta1.SearchIngester{})
	return nil
}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*componentsv1beta1.Payments] {
	return &Mutator{
		Client: client,
		Scheme: scheme,
	}
}
