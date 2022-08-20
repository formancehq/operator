package benthos

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal"
	"github.com/numary/formance-operator/internal/collectionutil"
	"github.com/numary/formance-operator/internal/resourceutil"
	pkgError "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultImage = "jeffail/benthos:latest"
	serverLabel  = "server.benthos.components.formance.com"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz")

func randPodIdentifier() string {
	b := make([]rune, 10)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=benthos.components.formance.com,resources=servers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=benthos.components.formance.com,resources=servers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=benthos.components.formance.com,resources=servers/finalizers,verbs=update

type ServerMutator struct {
	client client.Client
	scheme *runtime.Scheme
}

func (m *ServerMutator) SetupWithBuilder(mgr ctrl.Manager, blder *ctrl.Builder) error {
	blder.
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{})
	return nil
}

func (m *ServerMutator) Mutate(ctx context.Context, server *Server) (*ctrl.Result, error) {

	SetProgressing(server)

	pod, err := m.reconcilePod(ctx, server)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling pod")
	}

	_, err = m.reconcileService(ctx, server, pod)
	if err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	SetReady(server)

	return nil, nil
}

func (r *ServerMutator) reconcilePod(ctx context.Context, server *Server) (*corev1.Pod, error) {

	SetProgressing(server)

	image := server.Spec.Image
	if image == "" {
		image = defaultImage
	}

	expectedContainer := corev1.Container{
		Name:            "benthos",
		Image:           image,
		ImagePullPolicy: ImagePullPolicy(image),
		Command:         []string{"/benthos", "streams"},
		Ports: []corev1.ContainerPort{{
			Name:          "benthos",
			ContainerPort: 4195,
		}},
	}

	pods := &corev1.PodList{}
	requirement, err := labels.NewRequirement(serverLabel, selection.Equals, []string{server.Name})
	if err != nil {
		return nil, err
	}
	err = r.client.List(ctx, pods, &client.ListOptions{
		Namespace:     server.Namespace,
		LabelSelector: labels.NewSelector().Add(*requirement),
	})
	if err != nil {
		return nil, pkgError.Wrap(err, "listing pods with owner reference set to server")
	}
	if len(pods.Items) > 1 {
		return nil, pkgError.New("unexpected number of pods")
	}

	if len(pods.Items) == 1 {
		pod := pods.Items[0]
		if equality.Semantic.DeepDerivative(expectedContainer, pod.Spec.Containers[0]) {
			log.FromContext(ctx).Info("Pod up to date, skip update")
			if pod.Status.PodIP != "" {
				log.FromContext(ctx).Info("Detect pod ip, assign to server object", "ip", pod.Status.PodIP)
				server.Status.PodIP = pod.Status.PodIP
				SetCondition(server, "AssignedIP", metav1.ConditionTrue)
			}
			return &pod, nil
		}
		if err := r.client.Delete(ctx, &pod); err != nil {
			return nil, err
		}
	}

	RemovePodCondition(server)

	name := fmt.Sprintf("%s-%s", server.Name, randPodIdentifier())
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: server.Namespace,
		Name:      name,
	}, server, func(pod *corev1.Pod) error {
		matchLabels := collectionutil.CreateMap(
			"app.kubernetes.io/name", "benthos",
			serverLabel, server.Name,
		)

		image := server.Spec.Image
		if image == "" {
			image = defaultImage
		}
		pod.Labels = matchLabels
		pod.Spec.Containers = []corev1.Container{expectedContainer}
		return nil
	})
	switch {
	case err != nil:
		SetPodError(server, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		if err = r.client.Get(ctx, client.ObjectKeyFromObject(ret), ret); err != nil {
			return nil, pkgError.Wrap(err, "retrieving pod after creation")
		}

		log.FromContext(ctx).Info("Register pod ip", "ip", ret.Status.PodIP)
		server.Status.PodIP = ret.Status.PodIP
		SetPodReady(server)
	}
	return ret, err
}

func (r *ServerMutator) reconcileService(ctx context.Context, srv *Server, pod *corev1.Pod) (*corev1.Service, error) {
	ret, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, client.ObjectKeyFromObject(srv), srv, func(service *corev1.Service) error {
		service.Spec = corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:        "http",
				Port:        pod.Spec.Containers[0].Ports[0].ContainerPort,
				Protocol:    "TCP",
				AppProtocol: pointer.String("http"),
				TargetPort:  intstr.FromString(pod.Spec.Containers[0].Ports[0].Name),
			}},
			Selector: pod.Labels,
		}
		return nil
	})
	switch {
	case err != nil:
		SetServiceError(srv, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		SetServiceReady(srv)
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
