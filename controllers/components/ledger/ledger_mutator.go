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

package ledger

import (
	"context"
	"fmt"

	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal"
	"github.com/numary/formance-operator/internal/collectionutil"
	"github.com/numary/formance-operator/pkg/envutil"
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
	defaultImage = "ghcr.io/numary/ledger:latest"
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

func (r *Mutator) Mutate(ctx context.Context, ledger *componentsv1beta1.Ledger) (*ctrl.Result, error) {
	deployment, err := r.reconcileDeployment(ctx, ledger)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := r.reconcileService(ctx, ledger, deployment)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling service")
	}

	if ledger.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, ledger, service)
		if err != nil {
			return nil, pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ledger.Name,
				Namespace: ledger.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return nil, pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(ledger)
	}

	SetReady(ledger)

	return nil, nil
}

func (r *Mutator) reconcileDeployment(ctx context.Context, ledger *componentsv1beta1.Ledger) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.Create("app.kubernetes.io/name", "ledger")

	env := []corev1.EnvVar{
		envutil.Env("NUMARY_SERVER_HTTP_BIND_ADDRESS", "0.0.0.0:8080"),
		envutil.Env("NUMARY_STORAGE_DRIVER", "postgres"),
		envutil.Env("NUMARY_STORAGE_POSTGRES_CONN_STRING", ledger.Spec.Postgres.URI()),
	}
	if ledger.Spec.Debug {
		env = append(env, envutil.Env("NUMARY_DEBUG", "true"))
	}
	if ledger.Spec.Redis != nil {
		env = append(env, envutil.Env("NUMARY_REDIS_ENABLED", "true"))
		env = append(env, envutil.Env("NUMARY_REDIS_ADDR", ledger.Spec.Redis.Uri))
		if !ledger.Spec.Redis.TLS {
			env = append(env, envutil.Env("NUMARY_REDIS_USE_TLS", "false"))
		} else {
			env = append(env, envutil.Env("NUMARY_REDIS_USE_TLS", "true"))
		}
	}
	if ledger.Spec.Auth != nil {
		env = append(env, ledger.Spec.Auth.Env("NUMARY_")...)
	}
	if ledger.Spec.Monitoring != nil {
		env = append(env, ledger.Spec.Monitoring.Env("NUMARY_")...)
	}
	if ledger.Spec.Collector != nil {
		env = append(env, ledger.Spec.Collector.Env("NUMARY_")...)
	}

	image := ledger.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(ledger), ledger, func(deployment *appsv1.Deployment) error {
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
						Name:            "ledger",
						Image:           image,
						ImagePullPolicy: corev1.PullAlways,
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "ledger",
							ContainerPort: 8080,
						}},
					}},
				},
			},
		}
		if ledger.Spec.Postgres.CreateDatabase {
			deployment.Spec.Template.Spec.InitContainers = []corev1.Container{{
				Name:            "init-create-db-user",
				Image:           "postgres:13",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{
					"sh",
					"-c",
					fmt.Sprintf(`psql -Atx %s -c "SELECT 1 FROM pg_database WHERE datname = '%s'" | grep -q 1 && echo "Base already exists" || psql -Atx %s -c "CREATE DATABASE \"%s\""`,
						ledger.Spec.Postgres.URIWithoutDatabase(),
						ledger.Spec.Postgres.Database,
						ledger.Spec.Postgres.URIWithoutDatabase(),
						ledger.Spec.Postgres.Database,
					),
				},
				Env: []corev1.EnvVar{
					envutil.Env("NUMARY_STORAGE_POSTGRES_CONN_STRING", ledger.Spec.Postgres.URI()),
				},
			}}
		}
		return nil
	})
	switch {
	case err != nil:
		SetDeploymentError(ledger, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(ledger)
	}
	return ret, err
}

func (r *Mutator) reconcileService(ctx context.Context, auth *componentsv1beta1.Ledger, deployment *appsv1.Deployment) (*corev1.Service, error) {
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

func (r *Mutator) reconcileIngress(ctx context.Context, ledger *componentsv1beta1.Ledger, service *corev1.Service) (*networkingv1.Ingress, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(ledger), ledger, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = ledger.Spec.Ingress.Annotations
		ingress.Spec = networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: ledger.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     ledger.Spec.Ingress.Path,
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
		SetIngressError(ledger, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetIngressReady(ledger)
	}
	return ret, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Mutator) SetupWithBuilder(builder *ctrl.Builder) {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&authcomponentsv1beta1.Scope{})
}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*componentsv1beta1.Ledger] {
	return &Mutator{
		Client: client,
		Scheme: scheme,
	}
}
