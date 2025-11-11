package applications

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestWithNodeIP(t *testing.T) {
	t.Parallel()
	type testCase struct {
		name       string
		deployment *appsv1.Deployment
	}

	testCases := []testCase{
		{
			name: "empty deployment",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "main",
									Image: "test:latest",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "deployment with existing env vars",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "main",
									Image: "test:latest",
									Env: []v1.EnvVar{
										{Name: "EXISTING_VAR", Value: "existing_value"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "deployment with init containers",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							InitContainers: []v1.Container{
								{
									Name:  "init",
									Image: "init:latest",
								},
							},
							Containers: []v1.Container{
								{
									Name:  "main",
									Image: "test:latest",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "deployment with multiple containers",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							InitContainers: []v1.Container{
								{
									Name:  "init1",
									Image: "init1:latest",
								},
								{
									Name:  "init2",
									Image: "init2:latest",
								},
							},
							Containers: []v1.Container{
								{
									Name:  "main",
									Image: "test:latest",
								},
								{
									Name:  "sidecar",
									Image: "sidecar:latest",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app := Application{}
			require.NoError(t, app.withNodeIP(nil)(tc.deployment))

			// Verify NODE_IP is injected in all init containers
			for i, container := range tc.deployment.Spec.Template.Spec.InitContainers {
				found := false
				for _, env := range container.Env {
					if env.Name == "NODE_IP" {
						found = true
						require.NotNil(t, env.ValueFrom, "NODE_IP should use ValueFrom for init container %d", i)
						require.NotNil(t, env.ValueFrom.FieldRef, "NODE_IP should use FieldRef for init container %d", i)
						require.Equal(t, "status.hostIP", env.ValueFrom.FieldRef.FieldPath, "NODE_IP should reference status.hostIP for init container %d", i)
						break
					}
				}
				require.True(t, found, "NODE_IP env var not found in init container %d", i)
			}

			// Verify NODE_IP is injected in all containers
			for i, container := range tc.deployment.Spec.Template.Spec.Containers {
				found := false
				for _, env := range container.Env {
					if env.Name == "NODE_IP" {
						found = true
						require.NotNil(t, env.ValueFrom, "NODE_IP should use ValueFrom for container %d", i)
						require.NotNil(t, env.ValueFrom.FieldRef, "NODE_IP should use FieldRef for container %d", i)
						require.Equal(t, "status.hostIP", env.ValueFrom.FieldRef.FieldPath, "NODE_IP should reference status.hostIP for container %d", i)
						break
					}
				}
				require.True(t, found, "NODE_IP env var not found in container %d", i)
			}
		})
	}
}
