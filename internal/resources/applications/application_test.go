package applications

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
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

func TestContainersMutatorPreservesRestartedAtAnnotation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, v1.AddToScheme(scheme))

	type testCase struct {
		name               string
		existingAnnotation string
		expectedPreserved  bool
		deploymentTemplate *appsv1.Deployment
		initialDeployment  *appsv1.Deployment
		owner              v1beta1.Dependent
	}

	testCases := []testCase{
		{
			name:               "preserves restartedAt annotation when present",
			existingAnnotation: "2024-01-01T00:00:00Z",
			expectedPreserved:  true,
			deploymentTemplate: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-deployment",
				},
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "test",
									Image: "test:latest",
								},
							},
						},
					},
				},
			},
			initialDeployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								RestartedAtAnnotationKey: "2024-01-01T00:00:00Z",
								"other-annotation":       "other-value",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "old",
									Image: "old:latest",
								},
							},
						},
					},
				},
			},
			owner: &v1beta1.Ledger{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ledger",
				},
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: "test-stack",
					},
				},
			},
		},
		{
			name:               "does not add annotation when not present",
			existingAnnotation: "",
			expectedPreserved:  false,
			deploymentTemplate: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-deployment",
				},
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "test",
									Image: "test:latest",
								},
							},
						},
					},
				},
			},
			initialDeployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"other-annotation": "other-value",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "old",
									Image: "old:latest",
								},
							},
						},
					},
				},
			},
			owner: &v1beta1.Ledger{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ledger",
				},
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: "test-stack",
					},
				},
			},
		},
		{
			name:               "handles nil annotations",
			existingAnnotation: "",
			expectedPreserved:  false,
			deploymentTemplate: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-deployment",
				},
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "test",
									Image: "test:latest",
								},
							},
						},
					},
				},
			},
			initialDeployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "old",
									Image: "old:latest",
								},
							},
						},
					},
				},
			},
			owner: &v1beta1.Ledger{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ledger",
				},
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: "test-stack",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Create a fake client with empty settings (so GetStringOrDefault returns default)
			// We need to add indexes for Settings to match the real setup
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
					settings := obj.(*v1beta1.Settings)
					return settings.Spec.Stacks
				}).
				WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
					settings := obj.(*v1beta1.Settings)
					keys := strings.Split(settings.Spec.Key, ".")
					return []string{fmt.Sprint(len(keys))}
				}).
				Build()

			// Create a minimal manager for testing
			// We need to create a mock manager that implements the Manager interface
			mockMgr := &mockManager{
				client: fakeClient,
				scheme: scheme,
			}

			// Create a test context
			ctx := context.Background()
			testCtx := core.NewContext(mockMgr, ctx)

			// Create the Application with the owner and deployment template
			app := Application{
				owner:         tc.owner,
				deploymentTpl: tc.deploymentTemplate,
			}

			// Create deployment from initial state
			deployment := tc.initialDeployment.DeepCopy()
			deployment.SetName(tc.deploymentTemplate.Name)
			deployment.SetNamespace(tc.owner.GetStack())

			// Use the actual containersMutator function
			labels := map[string]string{
				"app.kubernetes.io/name": tc.deploymentTemplate.Name,
			}
			mutator := app.containersMutator(testCtx, labels)
			err := mutator(deployment)
			require.NoError(t, err, "containersMutator should not return an error")

			// Verify the annotation preservation
			if tc.expectedPreserved {
				require.NotNil(t, deployment.Spec.Template.Annotations, "annotations should not be nil")
				require.Equal(t, tc.existingAnnotation, deployment.Spec.Template.Annotations[RestartedAtAnnotationKey],
					"restartedAt annotation should be preserved")
			} else {
				if deployment.Spec.Template.Annotations != nil {
					_, exists := deployment.Spec.Template.Annotations[RestartedAtAnnotationKey]
					require.False(t, exists, "restartedAt annotation should not be present when not originally set")
				}
			}
		})
	}
}

func TestWithSemconvMetricsNames(t *testing.T) {
	//t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))

	stackName := "test-stack"
	deploymentName := "test-deployment"

	type testCase struct {
		name         string
		settingValue string
		deployment   *appsv1.Deployment
		expectEnvVar bool
	}

	testCases := []testCase{
		{
			name:         "setting disabled",
			settingValue: "false",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: deploymentName,
				},
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
			expectEnvVar: false,
		},
		{
			name:         "setting enabled - empty deployment",
			settingValue: "true",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: deploymentName,
				},
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
			expectEnvVar: true,
		},
		{
			name:         "setting enabled - deployment with existing env vars",
			settingValue: "true",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: deploymentName,
				},
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
			expectEnvVar: true,
		},
		{
			name:         "setting enabled - deployment with init containers",
			settingValue: "true",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: deploymentName,
				},
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
			expectEnvVar: true,
		},
		{
			name:         "setting enabled - deployment with multiple containers",
			settingValue: "true",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: deploymentName,
				},
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
			expectEnvVar: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Create setting for this test case
			settingKey := "deployments." + deploymentName + ".semconv-metrics-names"
			setting := &v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-semconv-setting",
				},
				Spec: v1beta1.SettingsSpec{
					Stacks: []string{stackName},
					Key:    settingKey,
					Value:  tc.settingValue,
				},
			}

			// Create fake client with the setting
			var clientObjs []client.Object
			if tc.settingValue != "" {
				clientObjs = append(clientObjs, setting)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(clientObjs...).
				WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
					return obj.(*v1beta1.Settings).GetStacks()
				}).
				WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
					key := obj.(*v1beta1.Settings).Spec.Key
					// Use the same logic as the actual indexer
					keyParts := settings.SplitKeywordWithDot(key)
					return []string{fmt.Sprintf("%d", len(keyParts))}
				}).
				Build()

			// Create mock context
			mockCtx := &mockContext{
				Context:  context.Background(),
				client:   fakeClient,
				scheme:   scheme,
				platform: core.Platform{Region: "testing", Environment: "testing"},
			}

			owner := &v1beta1.Ledger{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ledger",
				},
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stackName,
					},
				},
			}
			app := Application{
				owner: owner,
			}

			deploymentCopy := tc.deployment.DeepCopy()
			err := app.withSemconvMetricsNames(mockCtx)(deploymentCopy)
			require.NoError(t, err)

			if tc.expectEnvVar {
				// Verify SEMCONV_METRICS_NAME is injected in all init containers
				for i, container := range deploymentCopy.Spec.Template.Spec.InitContainers {
					found := false
					for _, env := range container.Env {
						if env.Name == "SEMCONV_METRICS_NAME" {
							found = true
							require.Equal(t, "true", env.Value, "SEMCONV_METRICS_NAME should be set to 'true' in init container %d", i)
							break
						}
					}
					require.True(t, found, "SEMCONV_METRICS_NAME env var not found in init container %d", i)
				}

				// Verify SEMCONV_METRICS_NAME is injected in all containers
				for i, container := range deploymentCopy.Spec.Template.Spec.Containers {
					found := false
					for _, env := range container.Env {
						if env.Name == "SEMCONV_METRICS_NAME" {
							found = true
							require.Equal(t, "true", env.Value, "SEMCONV_METRICS_NAME should be set to 'true' in container %d", i)
							break
						}
					}
					require.True(t, found, "SEMCONV_METRICS_NAME env var not found in container %d", i)
				}
			} else {
				// Verify SEMCONV_METRICS_NAME is NOT injected
				for i, container := range deploymentCopy.Spec.Template.Spec.InitContainers {
					for _, env := range container.Env {
						require.NotEqual(t, "SEMCONV_METRICS_NAME", env.Name, "SEMCONV_METRICS_NAME should not be set in init container %d when setting is false", i)
					}
				}
				for i, container := range deploymentCopy.Spec.Template.Spec.Containers {
					for _, env := range container.Env {
						require.NotEqual(t, "SEMCONV_METRICS_NAME", env.Name, "SEMCONV_METRICS_NAME should not be set in container %d when setting is false", i)
					}
				}
			}
		})
	}
}
