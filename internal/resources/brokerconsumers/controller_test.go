package brokerconsumers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type deletionTestContext struct {
	context.Context
	kubernetesClient client.Client
	scheme           *runtime.Scheme
}

func (d deletionTestContext) GetClient() client.Client    { return d.kubernetesClient }
func (d deletionTestContext) GetScheme() *runtime.Scheme  { return d.scheme }
func (d deletionTestContext) GetAPIReader() client.Reader { return d.kubernetesClient }
func (d deletionTestContext) GetPlatform() core.Platform  { return core.Platform{} }

func TestCleanupNatsConsumersCreatesModeSpecificCleanupJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		mode            v1beta1.Mode
		expectedCommand string
	}{
		{
			name:            "one stream by stack",
			mode:            v1beta1.ModeOneStreamByStack,
			expectedCommand: `consumer rm "$STACK" "$name" -f`,
		},
		{
			name:            "one stream by service",
			mode:            v1beta1.ModeOneStreamByService,
			expectedCommand: `consumer rm "$stream" "$QUERIED_BY" -f`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, batchv1.AddToScheme(scheme))
			require.NoError(t, v1beta1.AddToScheme(scheme))

			stack := &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{Name: "stack"},
				Spec:       v1beta1.StackSpec{Version: "v3.0.0"},
			}
			brokerURI, err := url.Parse("nats://nats.nats.svc.cluster.local:4222?replicas=1")
			require.NoError(t, err)
			broker := &v1beta1.Broker{
				ObjectMeta: metav1.ObjectMeta{Name: stack.Name},
				Spec: v1beta1.BrokerSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				},
				Status: v1beta1.BrokerStatus{
					URI:  &v1beta1.URI{URL: brokerURI},
					Mode: test.mode,
				},
			}
			module := &v1beta1.Webhooks{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1beta1.GroupVersion.String(),
					Kind:       "Webhooks",
				},
				ObjectMeta: metav1.ObjectMeta{Name: "webhooks", UID: types.UID("webhooks-uid")},
				Spec: v1beta1.WebhooksSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				},
			}
			consumer := &v1beta1.BrokerConsumer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1beta1.GroupVersion.String(),
					Kind:       "BrokerConsumer",
				},
				ObjectMeta: metav1.ObjectMeta{Name: "consumer", UID: types.UID("consumer-uid")},
				Spec: v1beta1.BrokerConsumerSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
					Services:        []string{"ledger", "payments"},
					QueriedBy:       "webhooks",
					Name:            "instance",
				},
			}
			require.NoError(t, controllerutil.SetControllerReference(module, consumer, scheme))

			kubernetesClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(stack, broker, module, consumer).
				WithIndex(&v1beta1.BrokerConsumer{}, "stack", func(object client.Object) []string {
					return []string{object.(*v1beta1.BrokerConsumer).Spec.Stack}
				}).
				WithIndex(&v1beta1.Settings{}, "stack", func(object client.Object) []string {
					return object.(*v1beta1.Settings).Spec.Stacks
				}).
				WithIndex(&v1beta1.Settings{}, "keylen", func(object client.Object) []string {
					return []string{fmt.Sprint(len(strings.Split(object.(*v1beta1.Settings).Spec.Key, ".")))}
				}).
				Build()
			ctx := deletionTestContext{
				Context:          context.Background(),
				kubernetesClient: kubernetesClient,
				scheme:           scheme,
			}

			err = CleanupNatsConsumers(ctx, module)
			require.Error(t, err)
			require.True(t, core.IsApplicationError(err))

			job := &batchv1.Job{}
			require.NoError(t, kubernetesClient.Get(ctx, types.NamespacedName{
				Namespace: stack.Name,
				Name:      string(module.UID) + "-delete-consumers-0",
			}, job))
			require.Equal(t, "docker.io/natsio/nats-box:0.19.2", job.Spec.Template.Spec.Containers[0].Image)
			require.Contains(t, job.Spec.Template.Spec.Containers[0].Args[2], test.expectedCommand)

			environment := map[string]string{}
			for _, variable := range job.Spec.Template.Spec.Containers[0].Env {
				environment[variable.Name] = variable.Value
			}
			require.Equal(t, "nats://nats.nats.svc.cluster.local:4222", environment["NATS_URI"])
			require.Equal(t, "stack", environment["STACK"])
			require.Equal(t, "webhooks", environment["QUERIED_BY"])
			require.Equal(t, "instance", environment["NAME"])
			require.Equal(t, "ledger payments", environment["SERVICES"])
			require.True(t, metav1.IsControlledBy(job, module))
		})
	}
}

func TestCleanupNatsConsumersIgnoresUnownedConsumers(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, batchv1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	module := &v1beta1.Webhooks{
		ObjectMeta: metav1.ObjectMeta{Name: "webhooks", UID: types.UID("webhooks-uid")},
		Spec: v1beta1.WebhooksSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack"},
		},
	}
	consumer := &v1beta1.BrokerConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer", UID: types.UID("consumer-uid")},
		Spec: v1beta1.BrokerConsumerSpec{
			StackDependency: v1beta1.StackDependency{Stack: module.GetStack()},
		},
	}
	kubernetesClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(module, consumer).
		WithIndex(&v1beta1.BrokerConsumer{}, "stack", func(object client.Object) []string {
			return []string{object.(*v1beta1.BrokerConsumer).Spec.Stack}
		}).
		Build()
	ctx := deletionTestContext{
		Context:          context.Background(),
		kubernetesClient: kubernetesClient,
		scheme:           scheme,
	}

	err := CleanupNatsConsumers(ctx, module)
	require.NoError(t, err)

	jobs := &batchv1.JobList{}
	require.NoError(t, kubernetesClient.List(ctx, jobs))
	require.Empty(t, jobs.Items)
}
