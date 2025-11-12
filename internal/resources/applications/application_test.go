package applications

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
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

// mockManager is a minimal implementation of core.Manager for testing
type mockManager struct {
	client client.Client
	scheme *runtime.Scheme
}

func (m *mockManager) GetClient() client.Client {
	return m.client
}

func (m *mockManager) GetScheme() *runtime.Scheme {
	return m.scheme
}

func (m *mockManager) GetAPIReader() client.Reader {
	return m.client
}

func (m *mockManager) GetCache() cache.Cache {
	return nil
}

func (m *mockManager) GetConfig() *rest.Config {
	return nil
}

func (m *mockManager) GetEventRecorderFor(string) record.EventRecorder {
	return nil
}

func (m *mockManager) GetLogger() logr.Logger {
	return logr.Discard()
}

func (m *mockManager) GetRESTMapper() meta.RESTMapper {
	return nil
}

func (m *mockManager) GetFieldIndexer() client.FieldIndexer {
	return nil
}

func (m *mockManager) GetHTTPClient() *http.Client {
	return nil
}

func (m *mockManager) GetWebhookServer() webhook.Server {
	return nil
}

func (m *mockManager) GetMetricsServer() server.Server {
	return nil
}

func (m *mockManager) GetControllerOptions() config.Controller {
	return config.Controller{}
}

func (m *mockManager) GetPlatform() core.Platform {
	return core.Platform{}
}

// Implement the required manager.Manager methods with minimal implementations
func (m *mockManager) Add(runnable manager.Runnable) error {
	return nil
}

func (m *mockManager) Elected() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (m *mockManager) SetFields(interface{}) error {
	return nil
}

func (m *mockManager) AddMetricsExtraHandler(string, interface{}) error {
	return nil
}

func (m *mockManager) AddHealthzCheck(string, healthz.Checker) error {
	return nil
}

func (m *mockManager) AddReadyzCheck(string, healthz.Checker) error {
	return nil
}

func (m *mockManager) Start(context.Context) error {
	return nil
}
