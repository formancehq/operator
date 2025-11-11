package services

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/go-libs/v2/pointer"
)

func TestWithAnnotations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		annotations    map[string]string
		initialService *corev1.Service
	}{
		{
			annotations: map[string]string{
				"test":    "value",
				"another": "value",
			},
			initialService: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
		},
		{
			annotations: map[string]string{
				"test":    "value",
				"another": "value",
			},
			initialService: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						"existing": "value",
						"another":  "oldValue",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run("Test with annotations", func(t *testing.T) {
			service := test.initialService
			err := withAnnotations(test.annotations)(service)
			require.NoError(t, err)

			for key, value := range test.annotations {
				require.Contains(t, service.Annotations, key)
				require.Equal(t, value, service.Annotations[key])
			}

			for key, value := range test.initialService.Annotations {
				require.Contains(t, service.Annotations, key)
				require.Equal(t, value, service.Annotations[key])
			}
		})
	}
}

func TestWithTrafficDistribution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		trafficDistribution string
		initialService      *corev1.Service
		expectedValue       *string
	}{
		{
			name:                "empty string should not set TrafficDistribution",
			trafficDistribution: "",
			initialService: &corev1.Service{
				Spec: corev1.ServiceSpec{},
			},
			expectedValue: nil,
		},
		{
			name:                "non-empty string should set TrafficDistribution",
			trafficDistribution: "PreferClose",
			initialService: &corev1.Service{
				Spec: corev1.ServiceSpec{},
			},
			expectedValue: pointer.For("PreferClose"),
		},
		{
			name:                "should overwrite existing TrafficDistribution",
			trafficDistribution: "PreferClose",
			initialService: &corev1.Service{
				Spec: corev1.ServiceSpec{
					TrafficDistribution: pointer.For("PreferSameZone"),
				},
			},
			expectedValue: pointer.For("PreferClose"),
		},
		{
			name:                "empty string should not modify existing TrafficDistribution",
			trafficDistribution: "",
			initialService: &corev1.Service{
				Spec: corev1.ServiceSpec{
					TrafficDistribution: pointer.For("PreferClose"),
				},
			},
			expectedValue: pointer.For("PreferClose"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := test.initialService.DeepCopy()
			err := withTrafficDistribution(test.trafficDistribution)(service)
			require.NoError(t, err)

			if test.expectedValue == nil {
				require.Nil(t, service.Spec.TrafficDistribution)
			} else {
				require.NotNil(t, service.Spec.TrafficDistribution)
				require.Equal(t, *test.expectedValue, *service.Spec.TrafficDistribution)
			}
		})
	}
}
