package benthos_components

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	benthosv1beta2 "github.com/numary/operator/apis/benthos.components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/typeutils"
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
	benthosImage = "jeffail/benthos:4.10.0"
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

func (m *ServerMutator) Mutate(ctx context.Context, server *benthosv1beta2.Server) (*ctrl.Result, error) {

	apisv1beta1.SetProgressing(server)

	pod, err := m.reconcilePod(ctx, server)
	if err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling pod")
	}

	_, err = m.reconcileService(ctx, server, pod)
	if err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling service")
	}

	apisv1beta1.SetReady(server)

	return nil, nil
}

func (r *ServerMutator) reconcilePod(ctx context.Context, server *benthosv1beta2.Server) (*corev1.Pod, error) {

	apisv1beta1.SetProgressing(server)

	command := []string{"/benthos"}
	if server.Spec.ResourcesConfigMap != "" {
		command = append(command, "-r", "/config/resources/*.yaml")
	}
	if server.Spec.TemplatesConfigMap != "" {
		command = append(command, "-t", "/config/templates/*.yaml")
	}
	if server.Spec.Dev {
		command = append(command, "--log.level", "trace")
	}
	command = append(command, "streams")
	if server.Spec.StreamsConfigMap != "" {
		command = append(command, "/config/streams/*.yaml")
	}

	expectedContainer := corev1.Container{
		Name:            "benthos",
		Image:           benthosImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         command,
		Ports: []corev1.ContainerPort{{
			Name:          "benthos",
			ContainerPort: 4195,
		}},
		VolumeMounts: []corev1.VolumeMount{},
		Env:          server.Spec.Env,
	}
	if server.Spec.TemplatesConfigMap != "" {
		expectedContainer.VolumeMounts = append(expectedContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "templates",
			ReadOnly:  true,
			MountPath: "/config/templates",
		})
	}
	if server.Spec.ResourcesConfigMap != "" {
		expectedContainer.VolumeMounts = append(expectedContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "resources",
			ReadOnly:  true,
			MountPath: "/config/resources",
		})
	}
	if server.Spec.StreamsConfigMap != "" {
		expectedContainer.VolumeMounts = append(expectedContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "streams",
			ReadOnly:  true,
			MountPath: "/config/streams",
		})
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
		if equality.Semantic.DeepDerivative(expectedContainer, pod.Spec.Containers[0]) &&
			equality.Semantic.DeepDerivative(server.Spec.InitContainers, pod.Spec.InitContainers) {
			log.FromContext(ctx).Info("Pod up to date, skip update")
			if pod.Status.PodIP != "" {
				log.FromContext(ctx).Info("Detect pod ip, assign to server object", "ip", pod.Status.PodIP)
				server.Status.PodIP = pod.Status.PodIP
				apisv1beta1.SetCondition(server, "AssignedIP", metav1.ConditionTrue)
			}
			return &pod, nil
		}
		if err := r.client.Delete(ctx, &pod); err != nil {
			return nil, err
		}
		apisv1beta1.RemoveCondition(server, "AssignedIP")
		server.Status.PodIP = ""
	}

	apisv1beta1.RemovePodCondition(server)

	name := fmt.Sprintf("%s-%s", server.Name, randPodIdentifier())
	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: server.Namespace,
		Name:      name,
	}, server, func(pod *corev1.Pod) error {
		matchLabels := CreateMap(
			"app.kubernetes.io/name", "benthos",
			serverLabel, server.Name,
		)

		pod.Spec.Volumes = []corev1.Volume{}
		if server.Spec.ResourcesConfigMap != "" {
			pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
				Name: "resources",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: server.Spec.ResourcesConfigMap,
						},
					},
				},
			})
		}
		if server.Spec.TemplatesConfigMap != "" {
			pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
				Name: "templates",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: server.Spec.TemplatesConfigMap,
						},
					},
				},
			})
		}
		if server.Spec.StreamsConfigMap != "" {
			pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
				Name: "streams",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: server.Spec.StreamsConfigMap,
						},
					},
				},
			})
		}

		pod.Labels = matchLabels
		pod.Spec.InitContainers = server.Spec.InitContainers
		pod.Spec.Containers = []corev1.Container{expectedContainer}

		return nil
	})
	switch {
	case err != nil:
		apisv1beta1.SetPodError(server, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		if err = r.client.Get(ctx, client.ObjectKeyFromObject(ret), ret); err != nil {
			return nil, pkgError.Wrap(err, "retrieving pod after creation")
		}

		log.FromContext(ctx).Info("Register pod ip", "ip", ret.Status.PodIP)
		server.Status.PodIP = ret.Status.PodIP
		apisv1beta1.SetPodReady(server)
	}
	return ret, err
}

func (r *ServerMutator) reconcileService(ctx context.Context, srv *benthosv1beta2.Server, pod *corev1.Pod) (*corev1.Service, error) {
	ret, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, client.ObjectKeyFromObject(srv), srv, func(service *corev1.Service) error {
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
		apisv1beta1.SetServiceError(srv, err.Error())
	case operationResult == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetServiceReady(srv)
	}
	return ret, err
}

var _ controllerutils.Mutator[*benthosv1beta2.Server] = &ServerMutator{}

func NewServerMutator(client client.Client, scheme *runtime.Scheme) controllerutils.Mutator[*benthosv1beta2.Server] {
	return &ServerMutator{
		client: client,
		scheme: scheme,
	}
}