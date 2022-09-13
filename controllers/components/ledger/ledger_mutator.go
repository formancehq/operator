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
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	authcomponentsv1beta1 "github.com/numary/operator/apis/components/auth/v1beta1"
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	"github.com/numary/operator/internal"
	"github.com/numary/operator/internal/collectionutil"
	"github.com/numary/operator/internal/resourceutil"
	pkgError "github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	autoscallingv1 "k8s.io/api/autoscaling/v1"
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
)

//go:embed benthos-config.yml
var benthosConfigDir embed.FS

var benthosConfigTpl = template.
	Must(
		template.New("benthos-config.yml").
			Funcs(template.FuncMap{
				"join": strings.Join,
			}).
			ParseFS(benthosConfigDir, "benthos-config.yml"),
	)

const (
	defaultImage = "ghcr.io/numary/ledger:latest"
)

// Mutator reconciles a Auth object
type Mutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=ledgers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=ledgers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=ledgers/finalizers,verbs=update

func (r *Mutator) Mutate(ctx context.Context, ledger *componentsv1beta1.Ledger) (*ctrl.Result, error) {

	SetProgressing(ledger)

	deployment, err := r.reconcileDeployment(ctx, ledger)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := r.reconcileService(ctx, ledger, deployment)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if err := r.reconcileSearchIngester(ctx, ledger); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if ledger.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, ledger, service)
		if err != nil {
			return Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ledger.Name,
				Namespace: ledger.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(ledger)
	}

	if _, err := r.reconcileHPA(ctx, ledger); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling HPA")
	}

	SetReady(ledger)

	return nil, nil
}

func (r *Mutator) reconcileDeployment(ctx context.Context, ledger *componentsv1beta1.Ledger) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.CreateMap("app.kubernetes.io/name", "ledger")

	env := []corev1.EnvVar{
		Env("NUMARY_SERVER_HTTP_BIND_ADDRESS", "0.0.0.0:8080"),
		Env("NUMARY_STORAGE_DRIVER", "postgres"),
	}
	env = append(env, ledger.Spec.Postgres.Env("NUMARY_")...)
	env = append(env, ledger.Spec.LockingStrategy.Env("NUMARY_")...)
	env = append(env, Env("NUMARY_STORAGE_POSTGRES_CONN_STRING", "$(NUMARY_POSTGRES_DATABASE_URI)"))
	if ledger.Spec.Debug {
		env = append(env, Env("NUMARY_DEBUG", "true"))
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

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(ledger), ledger, func(deployment *appsv1.Deployment) error {
		image := ledger.Spec.GetImage(defaultImage)
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: ledger.Spec.GetReplicas(),
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: ledger.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Name:            "ledger",
						Image:           image,
						ImagePullPolicy: ImagePullPolicy(image),
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "ledger",
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
		if ledger.Spec.Postgres.CreateDatabase {
			deployment.Spec.Template.Spec.InitContainers = []corev1.Container{{
				Name:            "init-create-ledger-db",
				Image:           "postgres:13",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{
					"sh",
					"-c",
					`psql -Atx ${POSTGRES_URI}/postgres -c "SELECT 1 FROM pg_database WHERE datname = '${POSTGRES_DATABASE}'" | grep -q 1 && echo "Base already exists" || psql -Atx ${POSTGRES_URI} -c "CREATE DATABASE \"${POSTGRES_DATABASE}\""`,
				},
				Env: ledger.Spec.Postgres.Env(""),
			}}
		}
		return nil
	})
	switch {
	case err != nil:
		SetDeploymentError(ledger, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(ledger)
	}

	selector, err := metav1.LabelSelectorAsSelector(ret.Spec.Selector)
	if err != nil {
		return nil, err
	}

	ledger.Status.Selector = selector.String()
	ledger.Status.Replicas = *ledger.Spec.GetReplicas()

	return ret, err
}

func (r *Mutator) reconcileHPA(ctx context.Context, ledger *componentsv1beta1.Ledger) (*autoscallingv2.HorizontalPodAutoscaler, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(ledger), ledger, func(hpa *autoscallingv2.HorizontalPodAutoscaler) error {
		hpa.Spec = ledger.Spec.GetHPASpec(ledger)
		return nil
	})
	switch {
	case err != nil:
		SetHPAError(ledger, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetHPAReady(ledger)
	}
	return ret, err
}

func (r *Mutator) reconcileService(ctx context.Context, ledger *componentsv1beta1.Ledger, deployment *appsv1.Deployment) (*corev1.Service, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(ledger), ledger, func(service *corev1.Service) error {
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
		SetServiceError(ledger, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetServiceReady(ledger)
	}
	return ret, err
}

func (r *Mutator) reconcileIngress(ctx context.Context, ledger *componentsv1beta1.Ledger, service *corev1.Service) (*networkingv1.Ingress, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(ledger), ledger, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = ledger.Spec.Ingress.Annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: ledger.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
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
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetIngressReady(ledger)
	}
	return ret, nil
}

func (r *Mutator) reconcileSearchIngester(ctx context.Context, ledger *componentsv1beta1.Ledger) error {
	_, ret, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, types.NamespacedName{
		Namespace: ledger.Namespace,
		Name:      ledger.Name + "-search-ingester",
	}, ledger, func(t *componentsv1beta1.SearchIngester) error {
		buf := bytes.NewBufferString("")
		if err := benthosConfigTpl.Execute(buf, map[string]any{
			"ElasticSearchIndex": ledger.Spec.ElasticSearchIndex,
		}); err != nil {
			return err
		}

		pipeline := map[string]any{}
		if err := yaml.Unmarshal(buf.Bytes(), &pipeline); err != nil {
			return err
		}

		data, err := json.Marshal(pipeline)
		if err != nil {
			return err
		}

		t.Spec.Pipeline = data
		t.Spec.Topic = ledger.Spec.Collector.Topic
		t.Spec.Reference = fmt.Sprintf("%s-search", ledger.Namespace)
		return nil
	})
	switch {
	case err != nil:
		SetCondition(ledger, "IngestionStreamReady", metav1.ConditionFalse, err.Error())
		return err
	case ret == controllerutil.OperationResultNone:
	default:
		SetCondition(ledger, "IngestionStreamReady", metav1.ConditionTrue)
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
		Owns(&componentsv1beta1.SearchIngester{}).
		Owns(&autoscallingv1.HorizontalPodAutoscaler{})
	return nil
}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*componentsv1beta1.Ledger] {
	return &Mutator{
		Client: client,
		Scheme: scheme,
	}
}
