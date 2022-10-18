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

package search

import (
	"context"
	"fmt"

	authcomponentsv1beta1 "github.com/numary/operator/apis/components/auth/v1beta1"
	. "github.com/numary/operator/apis/components/benthos/v1beta1"
	"github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	"github.com/numary/operator/internal"
	"github.com/numary/operator/internal/collectionutil"
	"github.com/numary/operator/internal/probeutil"
	"github.com/numary/operator/internal/resourceutil"
	"github.com/opensearch-project/opensearch-go"
	pkgError "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	autoscallingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultImage = "ghcr.io/formancehq/search:latest"
)

// Mutator reconciles a Auth object
type Mutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=searches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=searches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=searches/finalizers,verbs=update

func (r *Mutator) Mutate(ctx context.Context, search *v1beta1.Search) (*ctrl.Result, error) {
	deployment, err := r.reconcileDeployment(ctx, search)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling deployment")
	}

	if _, err = r.reconcileBenthosStreamServer(ctx, search); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling benthos stream server")
	}

	service, err := r.reconcileService(ctx, search, deployment)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if search.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, search, service)
		if err != nil {
			return Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      search.Name,
				Namespace: search.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(search)
	}

	if _, err := r.reconcileHPA(ctx, search); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling HPA")
	}

	SetReady(search)

	return nil, nil
}

func (r *Mutator) reconcileDeployment(ctx context.Context, search *v1beta1.Search) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.CreateMap("app.kubernetes.io/name", "search")

	env := []corev1.EnvVar{}
	if search.Spec.Monitoring != nil {
		env = append(env, search.Spec.Monitoring.Env("")...)
	}
	env = append(env, search.Spec.ElasticSearch.Env("")...)
	env = append(env, Env("ES_INDICES", search.Spec.Index))

	image := search.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(search), search, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: search.Spec.GetReplicas(),
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: search.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Name:            "search",
						Image:           image,
						ImagePullPolicy: ImagePullPolicy(image),
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "http",
							ContainerPort: 8080,
						}},
						LivenessProbe: probeutil.DefaultLiveness(),
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
								corev1.ResourceMemory: *resource.NewQuantity(256, resource.DecimalSI),
							},
						},
					}},
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		SetDeploymentError(search, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(search)
	}

	selector, err := metav1.LabelSelectorAsSelector(ret.Spec.Selector)
	if err != nil {
		return nil, err
	}

	search.Status.Selector = selector.String()
	search.Status.Replicas = *search.Spec.GetReplicas()

	return ret, err
}

func (r *Mutator) reconcileService(ctx context.Context, auth *v1beta1.Search, deployment *appsv1.Deployment) (*corev1.Service, error) {
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
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetServiceReady(auth)
	}
	return ret, err
}

func (r *Mutator) reconcileIngress(ctx context.Context, search *v1beta1.Search, service *corev1.Service) (*networkingv1.Ingress, error) {
	annotations := search.Spec.Ingress.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	middlewareAuth := fmt.Sprintf("%s-auth-middleware@kubernetescrd", search.Namespace)
	annotations["traefik.ingress.kubernetes.io/router.middlewares"] = fmt.Sprintf("%s, %s", middlewareAuth, annotations["traefik.ingress.kubernetes.io/router.middlewares"])
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(search), search, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: search.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
			Rules: []networkingv1.IngressRule{
				{
					Host: search.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     search.Spec.Ingress.Path,
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
		SetIngressError(search, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetIngressReady(search)
	}
	return ret, nil
}

func (r *Mutator) reconcileHPA(ctx context.Context, search *v1beta1.Search) (*autoscallingv2.HorizontalPodAutoscaler, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(search), search, func(hpa *autoscallingv2.HorizontalPodAutoscaler) error {
		hpa.Spec = search.Spec.GetHPASpec(search)
		return nil
	})
	switch {
	case err != nil:
		SetHPAError(search, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetHPAReady(search)
	}
	return ret, err
}

func (r *Mutator) reconcileBenthosStreamServer(ctx context.Context, search *v1beta1.Search) (controllerutil.OperationResult, error) {

	cfg := opensearch.Config{
		Addresses: []string{search.Spec.ElasticSearch.Endpoint()},
	}
	if search.Spec.ElasticSearch.BasicAuth != nil {
		cfg.Username = search.Spec.ElasticSearch.BasicAuth.Username
		cfg.Password = search.Spec.ElasticSearch.BasicAuth.Password
	}

	client, err := opensearch.NewClient(cfg)
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	if err := LoadMapping(ctx, client, DefaultMapping(search.Spec.Index)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	log.FromContext(ctx).Info("Mapping created es side")

	_, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, types.NamespacedName{
		Namespace: search.Namespace,
		Name:      search.Name + "-benthos",
	}, search, func(t *Server) error {
		return nil
	})
	switch {
	case err != nil:
		SetCondition(search, "BenthosReady", metav1.ConditionFalse, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetCondition(search, "BenthosReady", metav1.ConditionTrue)
	}
	return operationResult, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Mutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&authcomponentsv1beta1.Scope{}).
		Owns(&Server{})
	return nil
}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*v1beta1.Search] {
	return &Mutator{
		Client: client,
		Scheme: scheme,
	}
}
