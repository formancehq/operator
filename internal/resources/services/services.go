package services

import (
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
)

func WithDefault(name string) core.ObjectMutator[*corev1.Service] {
	return WithConfig(PortConfig{
		ServiceName: name,
		PortName:    "http",
		Port:        8080,
		TargetPort:  "http",
	})
}

type PortConfig struct {
	ServiceName string
	PortName    string
	Port        int32
	TargetPort  string
}

func WithConfig(cfg PortConfig) core.ObjectMutator[*corev1.Service] {
	return func(t *corev1.Service) error {
		if t.Labels == nil {
			t.Labels = make(map[string]string)
		}

		t.Labels["app.kubernetes.io/service-name"] = cfg.ServiceName
		t.Spec = corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       cfg.PortName,
				Port:       cfg.Port,
				Protocol:   "TCP",
				TargetPort: intstr.FromString(cfg.TargetPort),
			}},
			Selector: map[string]string{
				"app.kubernetes.io/name": cfg.ServiceName,
			},
		}

		return nil
	}
}

func withAnnotations(additionalAnnotation map[string]string) core.ObjectMutator[*corev1.Service] {
	return func(t *corev1.Service) error {
		if len(additionalAnnotation) == 0 {
			return nil
		}

		if t.Annotations == nil {
			t.Annotations = make(map[string]string)
		}

		maps.Copy(t.Annotations, additionalAnnotation)
		return nil
	}

}

func withTrafficDistribution(trafficDistribution string) core.ObjectMutator[*corev1.Service] {
	return func(t *corev1.Service) error {
		if trafficDistribution == "" {
			return nil
		}

		t.Spec.TrafficDistribution = &trafficDistribution

		return nil
	}
}

func Create(ctx core.Context, owner v1beta1.Dependent, serviceName string, mutators ...core.ObjectMutator[*corev1.Service]) (*corev1.Service, error) {
	additionalAnnotations, err := settings.GetMapOrEmpty(ctx, owner.GetStack(), "services", serviceName, "annotations")
	if err != nil {
		return nil, err
	}

	trafficDistribution, err := settings.GetStringOrEmpty(ctx, owner.GetStack(), "services", serviceName, "traffic-distribution")
	if err != nil {
		return nil, err
	}

	mutators = append(mutators,
		withAnnotations(additionalAnnotations),
		withTrafficDistribution(trafficDistribution),
		core.WithController[*corev1.Service](ctx.GetScheme(), owner),
	)

	service, _, err := core.CreateOrUpdate(ctx, types.NamespacedName{
		Name:      serviceName,
		Namespace: owner.GetStack(),
	}, mutators...)
	return service, err
}
