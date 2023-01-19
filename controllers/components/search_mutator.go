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
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	authcomponentsv1beta2 "github.com/formancehq/operator/apis/auth.components/v1beta2"
	benthosv1beta2 "github.com/formancehq/operator/apis/benthos.components/v1beta2"
	componentsv1beta2 "github.com/formancehq/operator/apis/components/v1beta2"
	apisv1beta2 "github.com/formancehq/operator/pkg/apis/v1beta2"
	"github.com/formancehq/operator/pkg/controllerutils"
	. "github.com/formancehq/operator/pkg/typeutils"
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

// Mutator reconciles a Auth object
type SearchMutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=searches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=searches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=searches/finalizers,verbs=update

func (r *SearchMutator) Mutate(ctx context.Context, search *componentsv1beta2.Search) (*ctrl.Result, error) {
	deployment, err := r.reconcileDeployment(ctx, search)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling deployment")
	}

	for _, dir := range []string{"templates", "streams", "resources", "global"} {
		if _, err = controllerutils.CreateConfigMapFromDir(ctx, types.NamespacedName{
			Namespace: search.Namespace,
			Name:      configMapName(dir),
		}, r.Client, benthosConfigDir, filepath.Join("benthos", dir),
			controllerutils.WithController[*corev1.ConfigMap](search, r.Scheme)); err != nil {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling benthos config")
		}
	}

	if _, err = r.reconcileBenthosStreamServer(ctx, search); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling benthos stream server")
	}

	service, err := r.reconcileService(ctx, search, deployment)
	if err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if search.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, search, service)
		if err != nil {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      search.Name,
				Namespace: search.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return controllerutils.Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		apisv1beta2.RemoveIngressCondition(search)
	}

	if _, err := r.reconcileHPA(ctx, search); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling HPA")
	}

	apisv1beta2.SetReady(search)

	return nil, nil
}

func (r *SearchMutator) reconcileDeployment(ctx context.Context, search *componentsv1beta2.Search) (*appsv1.Deployment, error) {
	matchLabels := CreateMap("app.kubernetes.io/name", "search")

	env := []corev1.EnvVar{}
	if search.Spec.Monitoring != nil {
		env = append(env, search.Spec.Monitoring.Env("")...)
	}
	if search.Spec.Debug {
		env = append(env, apisv1beta2.Env("DEBUG", "true"))
	}
	env = append(env, search.Spec.ElasticSearch.Env("")...)
	env = append(env, apisv1beta2.Env("ES_INDICES", search.Spec.Index))
	env = append(env, apisv1beta2.Env("MAPPING_INIT_DISABLED", "true"))

	ret, operationResult, err := controllerutils.CreateOrUpdate(ctx, r.Client, client.ObjectKeyFromObject(search),
		controllerutils.WithController[*appsv1.Deployment](search, r.Scheme),
		func(deployment *appsv1.Deployment) error {
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
						Containers: []corev1.Container{{
							Name:            "search",
							Image:           controllerutils.GetImage("search", search.Spec.Version),
							ImagePullPolicy: controllerutils.ImagePullPolicy(search.Spec),
							Env:             env,
							Ports: []corev1.ContainerPort{{
								Name:          "http",
								ContainerPort: 8080,
							}},
							LivenessProbe: controllerutils.DefaultLiveness(),
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    *resource.NewMilliQuantity(500, resource.DecimalSI),
									corev1.ResourceMemory: *resource.NewMilliQuantity(1024, resource.DecimalSI),
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
		apisv1beta2.SetDeploymentError(search, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta2.SetDeploymentReady(search)
	}

	selector, err := metav1.LabelSelectorAsSelector(ret.Spec.Selector)
	if err != nil {
		return nil, err
	}

	search.Status.Selector = selector.String()
	search.Status.Replicas = *search.Spec.GetReplicas()

	return ret, err
}

func (r *SearchMutator) reconcileService(ctx context.Context, search *componentsv1beta2.Search, deployment *appsv1.Deployment) (*corev1.Service, error) {
	ret, operationResult, err := controllerutils.CreateOrUpdate(ctx, r.Client, client.ObjectKeyFromObject(search),
		controllerutils.WithController[*corev1.Service](search, r.Scheme),
		func(service *corev1.Service) error {
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
		apisv1beta2.SetServiceError(search, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta2.SetServiceReady(search)
	}
	return ret, err
}

func (r *SearchMutator) reconcileIngress(ctx context.Context, search *componentsv1beta2.Search, service *corev1.Service) (*networkingv1.Ingress, error) {
	annotations := search.Spec.Ingress.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	middlewareAuth := fmt.Sprintf("%s-auth-middleware@kubernetescrd", search.Namespace)
	annotations["traefik.ingress.kubernetes.io/router.middlewares"] = fmt.Sprintf("%s, %s", middlewareAuth, annotations["traefik.ingress.kubernetes.io/router.middlewares"])
	ret, operationResult, err := controllerutils.CreateOrUpdate(ctx, r.Client, client.ObjectKeyFromObject(search),
		controllerutils.WithController[*networkingv1.Ingress](search, r.Scheme),
		func(ingress *networkingv1.Ingress) error {
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
		apisv1beta2.SetIngressError(search, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta2.SetIngressReady(search)
	}
	return ret, nil
}

func (r *SearchMutator) reconcileHPA(ctx context.Context, search *componentsv1beta2.Search) (*autoscallingv2.HorizontalPodAutoscaler, error) {
	ret, operationResult, err := controllerutils.CreateOrUpdate(ctx, r.Client, client.ObjectKeyFromObject(search),
		controllerutils.WithController[*autoscallingv2.HorizontalPodAutoscaler](search, r.Scheme),
		func(hpa *autoscallingv2.HorizontalPodAutoscaler) error {
			hpa.Spec = search.Spec.GetHPASpec(search)
			return nil
		})
	switch {
	case err != nil:
		apisv1beta2.SetHPAError(search, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta2.SetHPAReady(search)
	}
	return ret, err
}

func (r *SearchMutator) reconcileBenthosStreamServer(ctx context.Context, search *componentsv1beta2.Search) (controllerutil.OperationResult, error) {

	log.FromContext(ctx).Info("Mapping created es side")

	_, operationResult, err := controllerutils.CreateOrUpdate(ctx, r.Client, types.NamespacedName{
		Namespace: search.Namespace,
		Name:      search.Name + "-benthos",
	}, controllerutils.WithController[*benthosv1beta2.Server](search, r.Scheme),
		func(server *benthosv1beta2.Server) error {
			server.Spec.ResourcesConfigMap = configMapName("resources")
			server.Spec.TemplatesConfigMap = configMapName("templates")
			server.Spec.StreamsConfigMap = configMapName("streams")
			server.Spec.GlobalConfigMap = configMapName("global")
			server.Spec.ConfigurationFile = "config.yaml"
			server.Spec.DevProperties = search.Spec.DevProperties
			server.Spec.Env = []corev1.EnvVar{
				apisv1beta2.Env("KAFKA_ADDRESS", strings.Join(search.Spec.KafkaConfig.Brokers, ",")),
				// TODO: Rename search env vars
				//nolint:staticcheck
				apisv1beta2.Env("OPENSEARCH_URL", search.Spec.ElasticSearch.Endpoint()),
				apisv1beta2.Env("OPENSEARCH_INDEX", search.Spec.Index),
				apisv1beta2.Env("OPENSEARCH_BATCHING_COUNT", fmt.Sprint(search.Spec.Batching.Count)),
				apisv1beta2.Env("OPENSEARCH_BATCHING_PERIOD", search.Spec.Batching.Period),
				apisv1beta2.Env("TOPIC_PREFIX", search.Namespace+"-"),
			}
			if search.Spec.Monitoring != nil {
				server.Spec.Env = append(server.Spec.Env, search.Spec.Monitoring.Env("")...)
			}
			server.Spec.Env = append(server.Spec.Env, search.Spec.PostgresConfigs.Env()...)
			if search.Spec.ElasticSearch.BasicAuth != nil {
				server.Spec.Env = append(server.Spec.Env,
					apisv1beta2.Env("BASIC_AUTH_ENABLED", "true"),
					apisv1beta2.Env("BASIC_AUTH_USERNAME", search.Spec.ElasticSearch.BasicAuth.Username),
					apisv1beta2.Env("BASIC_AUTH_PASSWORD", search.Spec.ElasticSearch.BasicAuth.Password),
				)
			}
			if search.Spec.KafkaConfig.SASL != nil {
				server.Spec.Env = append(server.Spec.Env,
					apisv1beta2.Env("KAFKA_SASL_USERNAME", search.Spec.KafkaConfig.SASL.Username),
					apisv1beta2.Env("KAFKA_SASL_PASSWORD", search.Spec.KafkaConfig.SASL.Password),
					apisv1beta2.Env("KAFKA_SASL_MECHANISM", search.Spec.KafkaConfig.SASL.Mechanism),
				)
			}
			if search.Spec.KafkaConfig.TLS {
				server.Spec.Env = append(server.Spec.Env,
					apisv1beta2.Env("KAFKA_TLS_ENABLED", "true"),
				)
			}

			credentialsStr := ""
			if search.Spec.ElasticSearch.BasicAuth != nil {
				credentialsStr = "-u ${OPEN_SEARCH_USERNAME}:${OPEN_SEARCH_PASSWORD} "
			}
			initContainer := corev1.Container{
				Name:    "init-mapping",
				Image:   "curlimages/curl:7.86.0",
				Command: []string{"sh"},
				Env:     search.Spec.ElasticSearch.Env(""),
			}
			if search.Spec.ElasticSearch.UseZinc {
				mapping, err := json.Marshal(struct {
					Mappings any    `json:"mappings"`
					Name     string `json:"name"`
				}{
					Mappings: GetMapping(),
					Name:     search.Namespace,
				})
				if err != nil {
					return err
				}
				initContainer.Args = []string{
					"-c", fmt.Sprintf("curl -H 'Content-Type: application/json' "+
						"-X POST -v -d '%s' "+
						credentialsStr+
						"${OPEN_SEARCH_SCHEME}://${OPEN_SEARCH_SERVICE}/index", string(mapping)),
				}
			} else {
				mapping, err := json.Marshal(struct {
					Mappings any `json:"mappings"`
				}{
					Mappings: GetMapping(),
				})
				if err != nil {
					return err
				}
				initContainer.Args = []string{
					"-c", fmt.Sprintf("curl -H 'Content-Type: application/json' "+
						"-X PUT -v -d '%s' "+
						credentialsStr+
						"${OPEN_SEARCH_SCHEME}://${OPEN_SEARCH_SERVICE}/%s", string(mapping), search.Namespace),
				}
			}
			server.Spec.InitContainers = []corev1.Container{initContainer}

			return nil
		})
	switch {
	case err != nil:
		apisv1beta2.SetCondition(search, componentsv1beta2.ConditionTypeBenthosReady, metav1.ConditionFalse, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta2.SetCondition(search, componentsv1beta2.ConditionTypeBenthosReady, metav1.ConditionTrue)
	}
	return operationResult, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SearchMutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&authcomponentsv1beta2.Scope{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&benthosv1beta2.Server{})
	return nil
}

func NewSearchMutator(client client.Client, scheme *runtime.Scheme) controllerutils.Mutator[*componentsv1beta2.Search] {
	return &SearchMutator{
		Client: client,
		Scheme: scheme,
	}
}

func configMapName(directory string) string {
	return fmt.Sprintf("benthos-%s-config", directory)
}
