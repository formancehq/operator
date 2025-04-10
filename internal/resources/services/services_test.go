package services

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
