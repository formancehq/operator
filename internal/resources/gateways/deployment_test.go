package gateways

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type deploymentTestContext struct {
	context.Context
	client client.Client
	scheme *runtime.Scheme
}

func (c deploymentTestContext) GetClient() client.Client    { return c.client }
func (c deploymentTestContext) GetScheme() *runtime.Scheme  { return c.scheme }
func (c deploymentTestContext) GetAPIReader() client.Reader { return c.client }
func (c deploymentTestContext) GetPlatform() core.Platform  { return core.Platform{} }

func newDeploymentTestContext(t *testing.T, objects ...client.Object) deploymentTestContext {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	return deploymentTestContext{
		Context: context.Background(),
		client:  fakeClient,
		scheme:  scheme,
	}
}

func newBackendTLSHTTPAPIs(secretName string) []*v1beta1.GatewayHTTPAPI {
	return []*v1beta1.GatewayHTTPAPI{
		{
			Spec: v1beta1.GatewayHTTPAPISpec{
				Name: "ledger",
				Rules: []v1beta1.GatewayHTTPAPIRule{
					{
						Path: "/",
						BackendRef: &v1beta1.GatewayBackendRef{
							Name: "ledger",
							Port: 8080,
							TLS: &v1beta1.GatewayBackendTLS{
								SecretName: secretName,
								ServerName: "ledger",
							},
						},
					},
				},
			},
		},
	}
}

func newDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "gateway"}},
				},
			},
		},
	}
}

// When the backend TLS Secret has not been provisioned yet, the function must
// surface a pending (application) error so the framework retries rather than
// marking the Gateway as hard-failed.
func TestConfigureBackendTLSVolumesReturnsPendingWhenSecretMissing(t *testing.T) {
	t.Parallel()

	ctx := newDeploymentTestContext(t)
	deployment := newDeployment()

	err := configureBackendTLSVolumes(ctx, "stack0", deployment, newBackendTLSHTTPAPIs("backend-tls"), nil)

	require.Error(t, err)
	require.True(t, core.IsApplicationError(err),
		"expected a pending (application) error, got a hard error: %v", err)
	require.Empty(t, deployment.Spec.Template.Spec.Volumes,
		"no TLS volume should be mounted while the Secret is pending")
}

// When the backend TLS Secret exists, the function must proceed normally:
// return no error, mount the TLS volume and record the secrets hash annotation.
func TestConfigureBackendTLSVolumesProceedsWhenSecretExists(t *testing.T) {
	t.Parallel()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "stack0",
			Name:      "backend-tls",
		},
		Data: map[string][]byte{
			"ca.crt": []byte("ca-data"),
		},
	}
	ctx := newDeploymentTestContext(t, secret)
	deployment := newDeployment()

	err := configureBackendTLSVolumes(ctx, "stack0", deployment, newBackendTLSHTTPAPIs("backend-tls"), nil)

	require.NoError(t, err)
	require.Len(t, deployment.Spec.Template.Spec.Volumes, 1)
	require.Equal(t, "backend-tls", deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName)
	require.Len(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts, 1)
	require.NotEmpty(t, deployment.Spec.Template.Annotations["formance.com/backend-tls-secrets-hash"])
}

// A non-NotFound error (for example an RBAC or transport failure) must remain a
// hard error and never be masked as pending.
func TestConfigureBackendTLSVolumesReturnsHardErrorOnOtherFailures(t *testing.T) {
	t.Parallel()

	ctx := deploymentTestContext{
		Context: context.Background(),
		client:  failingGetClient{err: errForbidden{}},
		scheme:  runtime.NewScheme(),
	}
	deployment := newDeployment()

	err := configureBackendTLSVolumes(ctx, "stack0", deployment, newBackendTLSHTTPAPIs("backend-tls"), nil)

	require.Error(t, err)
	require.False(t, core.IsApplicationError(err),
		"a non-NotFound failure must stay a hard error, got a pending error: %v", err)
}

// errForbidden is an error that is NOT a Kubernetes NotFound error.
type errForbidden struct{}

func (errForbidden) Error() string { return "forbidden" }

// failingGetClient makes every Get fail with a non-NotFound error.
type failingGetClient struct {
	client.Client
	err error
}

func (c failingGetClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return c.err
}
