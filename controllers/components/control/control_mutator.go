package control

import (
	"context"
	"strings"

	. "github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	"github.com/numary/operator/internal"
	"github.com/numary/operator/internal/collectionutil"
	"github.com/numary/operator/internal/resourceutil"
	pkgError "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
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

const (
	defaultImage = "ghcr.io/formancehq/control:latest"
)

//+kubebuilder:rbac:groups=components.formance.com,resources=controls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=components.formance.com,resources=controls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=components.formance.com,resources=controls/finalizers,verbs=update

type Mutator struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func (m *Mutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	return nil
}

func (m *Mutator) Mutate(ctx context.Context, control *Control) (*ctrl.Result, error) {
	SetProgressing(control)

	deployment, err := m.reconcileDeployment(ctx, control)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := m.reconcileService(ctx, control, deployment)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	if control.Spec.Ingress != nil {
		_, err = m.reconcileIngress(ctx, control, service)
		if err != nil {
			return Requeue(), pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = m.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      control.Name,
				Namespace: control.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return Requeue(), pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(control)
	}

	if _, err := m.reconcileHPA(ctx, control); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling HPA")
	}

	SetReady(control)

	return nil, nil
}

func (m *Mutator) reconcileDeployment(ctx context.Context, control *Control) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.CreateMap("app.kubernetes.io/name", "control")

	env := []corev1.EnvVar{
		Env("API_URL_BACK", control.Spec.ApiURLBack),
		Env("API_URL_FRONT", control.Spec.ApiURLFront),
		Env("API_URL", control.Spec.ApiURLFront),
	}

	if control.Spec.Monitoring != nil {
		env = append(env, control.Spec.Monitoring.Env("")...)
	}

	// TODO: Generate value
	if control.Spec.AuthClientConfiguration != nil {
		env = append(env,
			Env("ENCRYPTION_KEY", "9h44y2ZqrDuUy5R9NGLA9hca7uRUr932"),
			Env("ENCRYPTION_IV", "b6747T6eP9DnMvEw"),
			Env("CLIENT_ID", control.Spec.AuthClientConfiguration.ClientID),
			Env("CLIENT_SECRET", control.Spec.AuthClientConfiguration.ClientSecret),
			// TODO: Clean that mess
			Env("REDIRECT_URI", strings.TrimSuffix(control.Spec.ApiURLFront, "/api")),
		)
	}

	image := control.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, m.Client, m.Scheme, client.ObjectKeyFromObject(control), control, func(deployment *appsv1.Deployment) error {
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: control.Spec.GetReplicas(),
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: matchLabels,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: control.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Name:            "control",
						Image:           image,
						ImagePullPolicy: ImagePullPolicy(image),
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "http",
							ContainerPort: 3000,
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    *resource.NewMilliQuantity(500, resource.DecimalSI),
								corev1.ResourceMemory: *resource.NewMilliQuantity(512, resource.DecimalSI),
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
		SetDeploymentError(control, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetDeploymentReady(control)
	}

	selector, err := metav1.LabelSelectorAsSelector(ret.Spec.Selector)
	if err != nil {
		return nil, err
	}

	control.Status.Selector = selector.String()
	control.Status.Replicas = *control.Spec.GetReplicas()

	return ret, err
}

func (m *Mutator) reconcileService(ctx context.Context, auth *Control, deployment *appsv1.Deployment) (*corev1.Service, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, m.Client, m.Scheme, client.ObjectKeyFromObject(auth), auth, func(service *corev1.Service) error {
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

func (m *Mutator) reconcileHPA(ctx context.Context, ctrl *Control) (*autoscallingv2.HorizontalPodAutoscaler, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, m.Client, m.Scheme, client.ObjectKeyFromObject(ctrl), ctrl, func(hpa *autoscallingv2.HorizontalPodAutoscaler) error {
		hpa.Spec = ctrl.Spec.GetHPASpec(ctrl)
		return nil
	})
	switch {
	case err != nil:
		SetHPAError(ctrl, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetHPAReady(ctrl)
	}
	return ret, err
}

func (m *Mutator) reconcileIngress(ctx context.Context, control *Control, service *corev1.Service) (*networkingv1.Ingress, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, m.Client, m.Scheme, client.ObjectKeyFromObject(control), control, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		ingress.ObjectMeta.Annotations = control.Spec.Ingress.Annotations
		ingress.Spec = networkingv1.IngressSpec{
			TLS: control.Spec.Ingress.TLS.AsK8SIngressTLSSlice(),
			Rules: []networkingv1.IngressRule{
				{
					Host: control.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     control.Spec.Ingress.Path,
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
		SetIngressError(control, err.Error())
		return nil, err
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetIngressReady(control)
	}
	return ret, nil
}

var _ internal.Mutator[*Control] = &Mutator{}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*Control] {
	return &Mutator{
		Client: client,
		Scheme: scheme,
	}
}
