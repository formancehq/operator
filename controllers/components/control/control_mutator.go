package control

import (
	"context"

	. "github.com/numary/formance-operator/apis/components/v1beta1"
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
	defaultImage = "ghcr.io/numary/control:latest"
)

//+kubebuilder:rbac:groups=components.formance.com,resources=controls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=components.formance.com,resources=controls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=components.formance.com,resources=controls/finalizers,verbs=update

type Mutator struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func (m *Mutator) SetupWithBuilder(builder *ctrl.Builder) {}

func (m *Mutator) Mutate(ctx context.Context, t *Control) (*ctrl.Result, error) {
	SetProgressing(t)

	deployment, err := m.reconcileDeployment(ctx, t)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling deployment")
	}

	service, err := m.reconcileService(ctx, t, deployment)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling service")
	}

	if t.Spec.Ingress != nil {
		_, err = m.reconcileIngress(ctx, t, service)
		if err != nil {
			return nil, pkgError.Wrap(err, "Reconciling service")
		}
	} else {
		err = m.Client.Delete(ctx, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      t.Name,
				Namespace: t.Namespace,
			},
		})
		if err != nil && !errors.IsNotFound(err) {
			return nil, pkgError.Wrap(err, "Deleting ingress")
		}
		RemoveIngressCondition(t)
	}

	SetReady(t)

	return nil, nil
}

func (m *Mutator) reconcileDeployment(ctx context.Context, control *Control) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.Create("app.kubernetes.io/name", "control")

	env := []corev1.EnvVar{
		envutil.Env("API_URL_BACK", "http://kubernetes.docker.internal"),
		envutil.Env("API_URL_FRONT", "http://kubernetes.docker.internal"),
	}

	image := control.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, m.Client, m.Scheme, client.ObjectKeyFromObject(control), control, func(deployment *appsv1.Deployment) error {
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
						Name:            "control",
						Image:           image,
						ImagePullPolicy: corev1.PullAlways,
						Env:             env,
						Ports: []corev1.ContainerPort{{
							Name:          "http",
							ContainerPort: 3000,
						}},
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

func (m *Mutator) reconcileIngress(ctx context.Context, control *Control, service *corev1.Service) (*networkingv1.Ingress, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, m.Client, m.Scheme, client.ObjectKeyFromObject(control), control, func(ingress *networkingv1.Ingress) error {
		pathType := networkingv1.PathTypePrefix
		// TODO: Disable because when testing, the path /ledgers of the front trigger the middleware which strip /ledger
		// ingress.ObjectMeta.Annotations = control.Spec.Ingress.Annotations
		ingress.Spec = networkingv1.IngressSpec{
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
