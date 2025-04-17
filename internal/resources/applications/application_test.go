package applications

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"
)

func TestWithAnnotations(t *testing.T) {
	t.Parallel()
	type testCase struct {
		annotations map[string]string
		deployment  *appsv1.Deployment
	}

	for _, tc := range []testCase{
		{
			annotations: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			deployment: &appsv1.Deployment{},
		},
		{
			annotations: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"existing-key": "existing-value",
							},
						},
					},
				},
			},
		},
	} {
		t.Run(t.Name(), func(t *testing.T) {
			t.Parallel()
			app := Application{}
			require.NoError(t, app.WithAnnotations(tc.annotations)(tc.deployment))

			for k, v := range tc.annotations {
				require.Equal(t, v, tc.deployment.Spec.Template.Annotations[k])
			}

			for k, v := range tc.deployment.Spec.Template.Annotations {
				if _, ok := tc.annotations[k]; !ok {
					require.Equal(t, v, tc.deployment.Spec.Template.Annotations[k])
				}
			}

		})
	}

}
