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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/formancehq/operator/apis/components/auth/v1beta1"
	componentsv1beta1 "github.com/formancehq/operator/apis/components/v1beta1"
	. "github.com/formancehq/operator/apis/sharedtypes"
	"github.com/formancehq/operator/internal"
	"github.com/formancehq/operator/internal/collectionutil"
	"github.com/formancehq/operator/internal/containerutil"
	"github.com/formancehq/operator/internal/probeutil"
	"github.com/formancehq/operator/internal/resourceutil"
	pkgError "github.com/pkg/errors"
	"gopkg.in/yaml.v3"
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
)

const (
	defaultImage = "ghcr.io/formancehq/auth:latest"
)

// Mutator reconciles a Auth object
type Mutator struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=auths,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.formance.com,resources=auths/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.formance.com,resources=auths/finalizers,verbs=update

func (r *Mutator) Mutate(ctx context.Context, auth *componentsv1beta1.Auth) (*ctrl.Result, error) {

	SetProgressing(auth)

	config, err := r.reconcileConfigFile(ctx, auth)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling config")
	}

	deployment, err := r.reconcileDeployment(ctx, auth, config)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := r.reconcileService(ctx, auth, deployment)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if auth.Spec.Ingress != nil {
		_, err = r.reconcileIngress(ctx, auth, service)
		if err != nil {
			return Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = r.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      auth.Name,
				Namespace: auth.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(auth)
	}

	if _, err := r.reconcileHPA(ctx, auth); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling HPA")
	}

	SetReady(auth)

	return nil, nil
}

func (r *Mutator) reconcileDeployment(ctx context.Context, auth *componentsv1beta1.Auth, config *corev1.ConfigMap) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.CreateMap("app.kubernetes.io/name", "auth")
	port := int32(8080)

	secret, err := r.reconcileSigningKeySecret(ctx, auth)
	if err != nil {
		return nil, err
	}

	env := make([]corev1.EnvVar, 0)
	env = append(env, Env("CONFIG", "/config/config.yaml"))
	env = append(env, auth.Spec.Postgres.Env("PG_")...)
	env = append(env, auth.Spec.DelegatedOIDCServer.Env()...)
	env = append(env,
		Env("POSTGRES_URI", "$(PG_POSTGRES_DATABASE_URI)"),
		Env("BASE_URL", auth.Spec.BaseURL),
		EnvFrom("SIGNING_KEY", &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secret.Name,
				},
				Key: "signingKey",
			},
		}),
	)
	if auth.Spec.DevMode {
		env = append(env,
			Env("DEBUG", "1"),
			Env("CAOS_OIDC_DEV", "1"),
		)
	}
	if auth.Spec.Monitoring != nil {
		env = append(env, auth.Spec.Monitoring.Env("")...)
	}

	image := auth.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: auth.Spec.GetReplicas(),
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: auth.Spec.ImagePullSecrets,
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: config.Name,
								},
							},
						},
					}},
					Containers: []corev1.Container{{
						Name:            "auth",
						Image:           image,
						Command:         []string{"/main", "serve"},
						Ports:           containerutil.SinglePort("http", port),
						Env:             env,
						LivenessProbe:   probeutil.DefaultLiveness(),
						ImagePullPolicy: ImagePullPolicy(image),
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
								corev1.ResourceMemory: *resource.NewMilliQuantity(256, resource.DecimalSI),
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config",
							ReadOnly:  true,
							MountPath: "/config",
						}},
					}},
				},
			},
		}
		if auth.Spec.Postgres.CreateDatabase {
			deployment.Spec.Template.Spec.InitContainers = []corev1.Container{{
				Name:            "init-create-auth-db",
				Image:           "postgres:13",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{
					"sh",
					"-c",
					`psql -Atx ${POSTGRES_URI}/postgres -c "SELECT 1 FROM pg_database WHERE datname = '${POSTGRES_DATABASE}'" | grep -q 1 && echo "Base already exists" || psql -Atx ${POSTGRES_URI}/postgres -c "CREATE DATABASE \"${POSTGRES_DATABASE}\""`,
				},
				Env: auth.Spec.Postgres.Env(""),
			}}
		}
		return nil
	})
	switch {
	case err != nil:
		SetDeploymentError(auth, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(auth)
	}

	selector, err := metav1.LabelSelectorAsSelector(ret.Spec.Selector)
	if err != nil {
		return nil, err
	}

	auth.Status.Selector = selector.String()
	auth.Status.Replicas = *auth.Spec.GetReplicas()

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
		SetServiceError(auth, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetServiceReady(auth)
	}
	return ret, err
}

func (r *Mutator) reconcileConfigFile(ctx context.Context, auth *componentsv1beta1.Auth) (*corev1.ConfigMap, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(configMap *corev1.ConfigMap) error {
		yaml, err := yaml.Marshal(struct {
			Clients []v1beta1.StaticClient `yaml:"clients"`
		}{
			Clients: auth.Spec.StaticClients,
		})
		if err != nil {
			panic(err)
		}
		configMap.Data = map[string]string{
			"config.yaml": string(yaml),
		}
		return nil
	})
	switch {
	case err != nil:
		SetConfigMapError(auth, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetConfigMapReady(auth)
	}
	return ret, err
}

func (r *Mutator) reconcileSigningKeySecret(ctx context.Context, auth *componentsv1beta1.Auth) (*corev1.Secret, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, types.NamespacedName{
		Namespace: auth.Namespace,
		Name:      fmt.Sprintf("%s-signing-key", auth.Name),
	}, auth, func(t *corev1.Secret) error {
		signingKey := auth.Spec.SigningKey
		if signingKey == "" {
			if _, ok := t.Data["signingKey"]; ok {
				return nil
			}
			privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				return err
			}
			var privateKeyBytes = x509.MarshalPKCS1PrivateKey(privateKey)
			privateKeyBlock := &pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: privateKeyBytes,
			}
			buf := bytes.NewBufferString("")
			err = pem.Encode(buf, privateKeyBlock)
			if err != nil {
				return err
			}
			signingKey = buf.String()
		}
		t.StringData = map[string]string{
			"signingKey": signingKey,
		}
		return nil
	})
	switch {
	case err != nil:
		SetSecretError(auth, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetSecretReady(auth)
	}
	return ret, err
}

func (r *Mutator) reconcileIngress(ctx context.Context, auth *componentsv1beta1.Auth, service *corev1.Service) (*networkingv1.Ingress, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = auth.Spec.Ingress.Annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: auth.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
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
		SetIngressError(auth, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetIngressReady(auth)
	}
	return ret, nil
}

func (r *Mutator) reconcileHPA(ctx context.Context, auth *componentsv1beta1.Auth) (*autoscallingv2.HorizontalPodAutoscaler, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.Client, r.Scheme, client.ObjectKeyFromObject(auth), auth, func(hpa *autoscallingv2.HorizontalPodAutoscaler) error {
		hpa.Spec = auth.Spec.GetHPASpec(auth)
		return nil
	})
	switch {
	case err != nil:
		SetHPAError(auth, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetHPAReady(auth)
	}
	return ret, err
}

// SetupWithBuilder sets up the controller with the Manager.
func (r *Mutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{})
	return nil
}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*componentsv1beta1.Auth] {
	return &Mutator{
		Client: client,
		Scheme: scheme,
	}
}
