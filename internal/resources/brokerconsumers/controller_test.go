package brokerconsumers

import (
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestCreateServiceNatsConsumerCreatesProvisioningJob(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	consumer := &v1beta1.BrokerConsumer{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "BrokerConsumer"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "ledger-worker",
			UID:  types.UID("consumer-uid"),
		},
		Spec: v1beta1.BrokerConsumerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Services:        []string{"ledger"},
			QueriedBy:       "worker",
		},
	}
	broker := &v1beta1.Broker{
		Status: v1beta1.BrokerStatus{
			URI: testutil.MustParseURI("nats://nats.stack0:4222"),
		},
	}
	ctx := testutil.NewContext(stack, consumer)

	err := createServiceNatsConsumer(ctx, stack, consumer, broker, "ledger")
	require.True(t, core.IsApplicationError(err))

	job := &batchv1.Job{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "consumer-uid-cc-ledger", Namespace: "stack0"}, job))
	require.Len(t, job.Spec.Template.Spec.Containers, 1)

	container := job.Spec.Template.Spec.Containers[0]
	envMap := testutil.EnvMap(container.Env)
	require.Contains(t, container.Image, "nats-box:0.19.2")
	require.Equal(t, "create-consumer", container.Name)
	require.Len(t, container.Args, 3)
	require.Contains(t, container.Args[2], "nats --server $NATS_URI consumer add $STACK-$SERVICE $NAME")
	require.Equal(t, "nats://nats.stack0:4222", envMap["NATS_URI"])
	require.Equal(t, "stack0", envMap["STACK"])
	require.Equal(t, "worker", envMap["NAME"])
	require.Equal(t, "ledger", envMap["SERVICE"])

	condition := consumer.Status.Conditions.Get(ConditionTypeNatsServiceConsumerCreated)
	require.NotNil(t, condition)
	require.Equal(t, "ledger", condition.Reason)
	require.Equal(t, metav1.ConditionFalse, condition.Status)
}

func TestCreateStackNatsConsumerCreatesProvisioningJob(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	consumer := &v1beta1.BrokerConsumer{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "BrokerConsumer"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ledger-worker",
			UID:        types.UID("consumer-uid"),
			Generation: 7,
		},
		Spec: v1beta1.BrokerConsumerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Services:        []string{"ledger", "payments"},
			QueriedBy:       "worker",
			Name:            "read",
		},
	}
	broker := &v1beta1.Broker{
		Status: v1beta1.BrokerStatus{
			URI: testutil.MustParseURI("nats://nats.stack0:4222"),
		},
	}
	ctx := testutil.NewContext(stack, consumer)

	err := createStackNatsConsumer(ctx, stack, consumer, broker)
	require.True(t, core.IsApplicationError(err))

	job := &batchv1.Job{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "consumer-uid-create-consumer", Namespace: "stack0"}, job))
	require.Len(t, job.Spec.Template.Spec.Containers, 1)

	container := job.Spec.Template.Spec.Containers[0]
	envMap := testutil.EnvMap(container.Env)
	require.Contains(t, container.Image, "nats-box:0.19.2")
	require.Equal(t, "create-consumer", container.Name)
	require.Len(t, container.Args, 3)
	require.Contains(t, container.Args[2], "nats --server $NATS_URI consumer add $STREAM $NAME")
	require.Equal(t, "nats://nats.stack0:4222", envMap["NATS_URI"])
	require.Equal(t, "stack0", envMap["STREAM"])
	require.Equal(t, "worker_read", envMap["NAME"])
	require.Equal(t, "worker", envMap["DELIVER"])
	require.Equal(t, "stack0.ledger stack0.payments", envMap["SUBJECTS"])

	condition := consumer.Status.Conditions.Get(ConditionTypeNatsStackConsumerCreated)
	require.NotNil(t, condition)
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	require.Equal(t, int64(7), condition.ObservedGeneration)
}
