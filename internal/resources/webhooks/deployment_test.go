package webhooks

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestDeploymentEnvVars(t *testing.T) {
	t.Parallel()

	stack, webhooks, database := webhooksFixtures()
	ctx := testutil.NewContext(
		settingspkg.New("broker-dsn", "broker.dsn", "nats://nats.stack0:4222?replicas=3", "stack0"),
		settingspkg.New("pool", "modules.webhooks.database.connection-pool", "max-idle=5", "stack0"),
	)

	env, err := deploymentEnvVars(ctx, stack, webhooks, database)
	require.NoError(t, err)

	envMap := testutil.EnvMap(env)
	require.Equal(t, "postgres.stack0", envMap["POSTGRES_HOST"])
	require.Equal(t, "webhooks", envMap["POSTGRES_DATABASE"])
	require.Equal(t, "$(POSTGRES_URI)", envMap["STORAGE_POSTGRES_CONN_STRING"])
	require.Equal(t, "nats.stack0:4222", envMap["PUBLISHER_NATS_URL"])
	require.Equal(t, "stack0-webhooks", envMap["PUBLISHER_NATS_CLIENT_ID"])
	require.Equal(t, "5", envMap["POSTGRES_MAX_IDLE_CONNS"])
	require.Equal(t, "true", envMap["DEBUG"])
	require.Equal(t, "true", envMap["DEV"])
}

func TestDeploymentEnvVarsRequiresBrokerDSN(t *testing.T) {
	t.Parallel()

	stack, webhooks, database := webhooksFixtures()
	_, err := deploymentEnvVars(testutil.NewContext(), stack, webhooks, database)
	require.Error(t, err)
	require.Contains(t, err.Error(), "settings 'broker.dsn' not found")
}

func TestCreateSingleDeployment(t *testing.T) {
	t.Parallel()

	stack, webhooks, database := webhooksFixtures()
	consumer := &v1beta1.BrokerConsumer{
		ObjectMeta: testutil.ObjectMeta("webhooks"),
		Spec: v1beta1.BrokerConsumerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Services:        []string{"ledger", "payments"},
		},
		Status: v1beta1.BrokerConsumerStatus{Status: v1beta1.Status{Ready: true}},
	}
	broker := &v1beta1.Broker{
		ObjectMeta: testutil.ObjectMeta("stack0"),
		Spec:       v1beta1.BrokerSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
		Status: v1beta1.BrokerStatus{
			Status: v1beta1.Status{Ready: true},
			Mode:   v1beta1.ModeOneStreamByStack,
			URI:    testutil.MustParseURI("nats://nats.stack0:4222"),
		},
	}
	ctx := testutil.NewContext(
		webhooks,
		broker,
		settingspkg.New("broker-dsn", "broker.dsn", "nats://nats.stack0:4222", "stack0"),
	)

	require.NoError(t, createSingleDeployment(ctx, stack, webhooks, database, consumer, "v2.0.0-rc.4"))

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "webhooks", Namespace: "stack0"}, deployment))
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)

	container := deployment.Spec.Template.Spec.Containers[0]
	envMap := testutil.EnvMap(container.Env)
	require.Equal(t, "api", container.Name)
	require.Equal(t, "ghcr.io/formancehq/webhooks:v2.0.0-rc.4", container.Image)
	require.Equal(t, []string{"serve", "--auto-migrate"}, container.Args)
	require.Equal(t, "true", envMap["WORKER"])
	require.Equal(t, "stack0.ledger stack0.payments", envMap["KAFKA_TOPICS"])
	require.Equal(t, "false", envMap["PUBLISHER_NATS_AUTO_PROVISION"])
	require.NotNil(t, container.LivenessProbe)
	require.NotNil(t, container.ReadinessProbe)
	require.Equal(t, "webhooks", deployment.Spec.Template.Labels["app.kubernetes.io/name"])
}

func webhooksFixtures() (*v1beta1.Stack, *v1beta1.Webhooks, *v1beta1.Database) {
	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack0"},
		Spec:       v1beta1.StackSpec{DevProperties: v1beta1.DevProperties{Debug: true}},
	}
	webhooks := &v1beta1.Webhooks{
		ObjectMeta: testutil.ObjectMeta("webhooks"),
		Spec: v1beta1.WebhooksSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			ModuleProperties: v1beta1.ModuleProperties{
				DevProperties: v1beta1.DevProperties{Dev: true},
			},
		},
	}
	database := &v1beta1.Database{
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "webhooks",
		},
		Status: v1beta1.DatabaseStatus{
			URI:      testutil.MustParseURI("postgresql://webhooks:secret@postgres.stack0:5432/webhooks"),
			Database: "webhooks",
		},
	}
	return stack, webhooks, database
}
