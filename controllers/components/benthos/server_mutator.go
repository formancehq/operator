package benthos

import (
	"context"

	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	"github.com/numary/formance-operator/internal"
	"github.com/numary/formance-operator/internal/collectionutil"
	"github.com/numary/formance-operator/pkg/resourceutil"
	pkgError "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	defaultImage = "jeffail/benthos:latest"
)

//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=servers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=servers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=servers/finalizers,verbs=update

type ServerMutator struct {
	client client.Client
	scheme *runtime.Scheme
}

func (m *ServerMutator) SetupWithBuilder(builder *ctrl.Builder) {
	builder.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{})
}

func (m *ServerMutator) Mutate(ctx context.Context, server *Server) (*ctrl.Result, error) {

	server.Progress()

	deployment, err := m.reconcileDeployment(ctx, server)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling deployment")
	}

	_, err = m.reconcileService(ctx, server, deployment)
	if err != nil {
		return nil, pkgError.Wrap(err, "Reconciling service")
	}

	server.SetReady()

	return nil, nil
}

func (r *ServerMutator) reconcileDeployment(ctx context.Context, m *Server) (*appsv1.Deployment, error) {
	matchLabels := collectionutil.Create("app.kubernetes.io/name", "benthos")

	image := m.Spec.Image
	if image == "" {
		image = defaultImage
	}

	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, client.ObjectKeyFromObject(m), m, func(deployment *appsv1.Deployment) error {
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
						Name:            "benthos",
						Image:           image,
						ImagePullPolicy: corev1.PullAlways,
						Command:         []string{"/benthos", "streams"},
						Ports: []corev1.ContainerPort{{
							Name:          "benthos",
							ContainerPort: 4195,
						}},
					}},
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		m.SetDeploymentFailure(err)
	case operationResult == controllerutil.OperationResultNone:
	default:
		m.SetDeploymentCreated()
	}
	return ret, err
}

func (r *ServerMutator) reconcileService(ctx context.Context, srv *Server, deployment *appsv1.Deployment) (*corev1.Service, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, client.ObjectKeyFromObject(srv), srv, func(service *corev1.Service) error {
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
		srv.SetServiceFailure(err)
	case operationResult == controllerutil.OperationResultNone:
	default:
		srv.SetServiceCreated()
	}
	return ret, err
}

var _ internal.Mutator[*Server] = &ServerMutator{}

func NewServerMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*Server] {
	return &ServerMutator{
		client: client,
		scheme: scheme,
	}
}
