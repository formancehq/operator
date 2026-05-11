package brokers

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestCreateNatsTopicCreatesProvisioningJob(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	broker := brokerFixture(v1beta1.ModeOneStreamByService)
	topic := &v1beta1.BrokerTopic{
		ObjectMeta: testutil.ObjectMeta("topic0"),
		Spec: v1beta1.BrokerTopicSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "ledger",
		},
	}
	ctx := testutil.NewContext(stack, broker)

	err := createNatsTopic(ctx, stack, broker, topic, testutil.MustParseURI("nats://nats.stack0:4222?replicas=3"))
	require.True(t, core.IsApplicationError(err))

	job := &batchv1.Job{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "broker-uid-create-topic-ledger", Namespace: "stack0"}, job))
	require.Len(t, job.Spec.Template.Spec.Containers, 1)

	container := job.Spec.Template.Spec.Containers[0]
	envMap := testutil.EnvMap(container.Env)
	require.Contains(t, container.Image, "nats-box:0.19.2")
	require.Equal(t, "create-topic", container.Name)
	require.Equal(t, "nats://nats.stack0:4222", envMap["NATS_URI"])
	require.Equal(t, "stack0-ledger", envMap["SUBJECT"])
	require.Equal(t, "stack0-ledger", envMap["STREAM"])
	require.Equal(t, "3", envMap["REPLICAS"])
}

func TestCreateOneStreamByStackCreatesProvisioningJob(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	broker := brokerFixture(v1beta1.ModeOneStreamByStack)
	ctx := testutil.NewContext(stack, broker)

	err := createOneStreamByStack(ctx, stack, broker, testutil.MustParseURI("nats://nats.stack0:4222?replicas=2"))
	require.True(t, core.IsApplicationError(err))

	job := &batchv1.Job{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "broker-uid-create-stream", Namespace: "stack0"}, job))
	require.Len(t, job.Spec.Template.Spec.Containers, 1)

	container := job.Spec.Template.Spec.Containers[0]
	envMap := testutil.EnvMap(container.Env)
	require.Contains(t, container.Image, "nats-box:0.19.2")
	require.Equal(t, "create-topic", container.Name)
	require.Equal(t, "nats://nats.stack0:4222", envMap["NATS_URI"])
	require.Equal(t, "stack0", envMap["STREAM"])
	require.Equal(t, "2", envMap["REPLICAS"])
}

func TestCreateOneStreamByStackSkipsReadyBroker(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	broker := brokerFixture(v1beta1.ModeOneStreamByStack)
	broker.Status.Ready = true

	require.NoError(t, createOneStreamByStack(testutil.NewContext(stack, broker), stack, broker, testutil.MustParseURI("nats://nats.stack0:4222")))
}

func TestCreateOneStreamByTopicCreatesMissingTopicAndSortsStreams(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	broker := brokerFixture(v1beta1.ModeOneStreamByService)
	broker.Status.Streams = []string{"wallets"}
	ledgerTopic := &v1beta1.BrokerTopic{
		ObjectMeta: testutil.ObjectMeta("ledger-topic"),
		Spec: v1beta1.BrokerTopicSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "ledger",
		},
	}
	walletsTopic := &v1beta1.BrokerTopic{
		ObjectMeta: testutil.ObjectMeta("wallets-topic"),
		Spec: v1beta1.BrokerTopicSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "wallets",
		},
	}
	ctx := newBrokerContextWithTopicIndexes(stack, broker, ledgerTopic, walletsTopic)

	err := createOneStreamByTopic(ctx, stack, broker, testutil.MustParseURI("nats://nats.stack0:4222?replicas=4"))
	require.Error(t, err)
	require.True(t, core.IsApplicationError(err), err)
	require.Equal(t, []string{"wallets"}, broker.Status.Streams)

	job := &batchv1.Job{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "broker-uid-create-topic-ledger", Namespace: "stack0"}, job))
	envMap := testutil.EnvMap(job.Spec.Template.Spec.Containers[0].Env)
	require.Equal(t, "stack0-ledger", envMap["SUBJECT"])
	require.Equal(t, "4", envMap["REPLICAS"])
}

func TestCreateOneStreamByTopicMarksStreamsWhenProvisioningJobSucceeded(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	broker := brokerFixture(v1beta1.ModeOneStreamByService)
	broker.Status.Streams = []string{"wallets"}
	ledgerTopic := &v1beta1.BrokerTopic{
		ObjectMeta: testutil.ObjectMeta("ledger-topic"),
		Spec: v1beta1.BrokerTopicSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "ledger",
		},
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "broker-uid-create-topic-ledger",
			Namespace: "stack0",
		},
		Status: batchv1.JobStatus{Succeeded: 1},
	}
	ctx := newBrokerContextWithTopicIndexes(stack, broker, ledgerTopic, job)

	require.NoError(t, createOneStreamByTopic(ctx, stack, broker, testutil.MustParseURI("nats://nats.stack0:4222")))
	require.Equal(t, []string{"ledger", "wallets"}, broker.Status.Streams)
}

func TestHasAllVersionsGreaterThan(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		stack    *v1beta1.Stack
		objects  []client.Object
		expected bool
	}{
		{
			name: "explicit version greater",
			stack: &v1beta1.Stack{
				Spec: v1beta1.StackSpec{Version: "v2.0.0"},
			},
			expected: true,
		},
		{
			name: "explicit version older",
			stack: &v1beta1.Stack{
				Spec: v1beta1.StackSpec{Version: "v1.9.0"},
			},
			expected: false,
		},
		{
			name: "invalid explicit version is treated as latest",
			stack: &v1beta1.Stack{
				Spec: v1beta1.StackSpec{Version: "latest"},
			},
			expected: true,
		},
		{
			name: "versions file all greater ignoring control",
			stack: &v1beta1.Stack{
				Spec: v1beta1.StackSpec{VersionsFromFile: "versions"},
			},
			objects: []client.Object{&v1beta1.Versions{
				ObjectMeta: testutil.ObjectMeta("versions"),
				Spec: map[string]string{
					"ledger":  "v2.0.0",
					"control": "v1.0.0",
				},
			}},
			expected: true,
		},
		{
			name: "versions file with older service",
			stack: &v1beta1.Stack{
				Spec: v1beta1.StackSpec{VersionsFromFile: "versions"},
			},
			objects: []client.Object{&v1beta1.Versions{
				ObjectMeta: testutil.ObjectMeta("versions"),
				Spec: map[string]string{
					"ledger":   "v2.0.0",
					"payments": "v1.9.0",
				},
			}},
			expected: false,
		},
		{
			name:     "no version defaults to latest",
			stack:    &v1beta1.Stack{},
			expected: true,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d-%s", i, tc.name), func(t *testing.T) {
			t.Parallel()

			ctx := testutil.NewContext(tc.objects...)
			actual, err := hasAllVersionsGreaterThan(ctx, tc.stack, "v2.0.0-rc.27")
			require.NoError(t, err)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestDeleteBrokerCreatesDeleteStreamsJobForReadyNatsBroker(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	broker := brokerFixture(v1beta1.ModeOneStreamByStack)
	broker.Status.Ready = true
	broker.Status.URI = testutil.MustParseURI("nats://nats.stack0:4222")
	ctx := testutil.NewContext(stack, broker)

	err := deleteBroker(ctx, broker)
	require.True(t, core.IsApplicationError(err))

	job := &batchv1.Job{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "broker-uid-delete-streams", Namespace: "stack0"}, job))
	container := job.Spec.Template.Spec.Containers[0]
	require.Equal(t, "delete-streams", container.Name)
	require.Equal(t, "stack0", testutil.EnvMap(container.Env)["STACK"])
	require.Len(t, container.Args, 3)
	require.Contains(t, container.Args[2], "nats stream rm")
}

func TestDeleteBrokerSkipsNonReadyOrNonNatsBroker(t *testing.T) {
	t.Parallel()

	require.NoError(t, deleteBroker(testutil.NewContext(), &v1beta1.Broker{
		Status: v1beta1.BrokerStatus{Status: v1beta1.Status{Ready: false}},
	}))

	broker := brokerFixture(v1beta1.ModeOneStreamByStack)
	broker.Status.Ready = true
	broker.Status.URI = testutil.MustParseURI("kafka://kafka.stack0:9092")
	require.NoError(t, deleteBroker(testutil.NewContext(), broker))
}

func brokerFixture(mode v1beta1.Mode) *v1beta1.Broker {
	return &v1beta1.Broker{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Broker"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "stack0",
			UID:  types.UID("broker-uid"),
		},
		Spec: v1beta1.BrokerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
		Status: v1beta1.BrokerStatus{Mode: mode},
	}
}

func newBrokerContextWithTopicIndexes(objects ...client.Object) *testutil.Context {
	scheme := testutil.NewScheme()
	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...)
	builder.WithIndex(&v1beta1.BrokerTopic{}, "stack", func(obj client.Object) []string {
		topic := obj.(*v1beta1.BrokerTopic)
		if topic.Spec.Stack == "" {
			return nil
		}
		return []string{topic.Spec.Stack}
	})
	builder.WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
		return obj.(*v1beta1.Settings).GetStacks()
	})
	builder.WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
		return []string{"0"}
	})

	return &testutil.Context{
		Context: context.Background(),
		Client:  builder.Build(),
		Scheme:  scheme,
	}
}
