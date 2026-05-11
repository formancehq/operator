package brokers

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestGetBrokerEnvVarsKafka(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(settings.New("aws", "aws.service-account", "aws-sa", "stack0"))
	uri := testutil.MustParseURI("kafka://kafka:9092?saslEnabled=true&saslUsername=user&saslPassword=pass&saslMechanism=SCRAM-SHA-512&saslSCRAMSHASize=512&tls=true&circuitBreakerEnabled=true&circuitBreakerOpenInterval=10s")

	env, err := GetBrokerEnvVars(ctx, uri, "stack0", "ledger")
	require.NoError(t, err)

	values := testutil.EnvMap(env)
	require.Equal(t, "kafka", values["BROKER"])
	require.Equal(t, "true", values["PUBLISHER_KAFKA_ENABLED"])
	require.Equal(t, "kafka:9092", values["PUBLISHER_KAFKA_BROKER"])
	require.Equal(t, "true", values["PUBLISHER_KAFKA_SASL_ENABLED"])
	require.Equal(t, "user", values["PUBLISHER_KAFKA_SASL_USERNAME"])
	require.Equal(t, "pass", values["PUBLISHER_KAFKA_SASL_PASSWORD"])
	require.Equal(t, "SCRAM-SHA-512", values["PUBLISHER_KAFKA_SASL_MECHANISM"])
	require.Equal(t, "512", values["PUBLISHER_KAFKA_SASL_SCRAM_SHA_SIZE"])
	require.Equal(t, "true", values["PUBLISHER_KAFKA_SASL_IAM_ENABLED"])
	require.Equal(t, "true", values["PUBLISHER_KAFKA_TLS_ENABLED"])
	require.Equal(t, "true", values["PUBLISHER_CIRCUIT_BREAKER_ENABLED"])
	require.Equal(t, "10s", values["PUBLISHER_CIRCUIT_BREAKER_OPEN_INTERVAL_DURATION"])
}

func TestGetBrokerEnvVarsNatsAndPublisherMappings(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext()
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	uri := testutil.MustParseURI("nats://nats:4222")

	env, err := GetBrokerEnvVars(ctx, uri, "stack0", "payments")
	require.NoError(t, err)
	values := testutil.EnvMap(env)
	require.Equal(t, "nats", values["BROKER"])
	require.Equal(t, "true", values["PUBLISHER_NATS_ENABLED"])
	require.Equal(t, "nats:4222", values["PUBLISHER_NATS_URL"])
	require.Equal(t, "stack0-payments", values["PUBLISHER_NATS_CLIENT_ID"])

	broker := &v1beta1.Broker{Status: v1beta1.BrokerStatus{Mode: v1beta1.ModeOneStreamByService, URI: uri}}
	require.Equal(t, "*:stack0-payments", testutil.EnvMap(GetPublisherEnvVars(stack, broker, "payments"))["PUBLISHER_TOPIC_MAPPING"])

	broker.Status.Mode = v1beta1.ModeOneStreamByStack
	require.Equal(t, map[string]string{
		"PUBLISHER_TOPIC_MAPPING":       "*:stack0.payments",
		"PUBLISHER_NATS_AUTO_PROVISION": "false",
	}, testutil.EnvMap(GetPublisherEnvVars(stack, broker, "payments")))
}

func TestGetTopicsEnvVars(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(&v1beta1.Broker{
		TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Broker"},
		ObjectMeta: testutil.ObjectMeta("stack0"),
		Status: v1beta1.BrokerStatus{
			Status: v1beta1.Status{Ready: true},
			Mode:   v1beta1.ModeOneStreamByStack,
			URI:    testutil.MustParseURI("nats://nats:4222"),
		},
	})
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}

	env, err := GetTopicsEnvVars(ctx, stack, "TOPICS", "ledger", "payments")
	require.NoError(t, err)

	require.Equal(t, map[string]string{
		"TOPICS":                        "stack0.ledger stack0.payments",
		"PUBLISHER_NATS_AUTO_PROVISION": "false",
	}, testutil.EnvMap(env))
}

func TestGetTopicsEnvVarsPendingBroker(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(&v1beta1.Broker{
		TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Broker"},
		ObjectMeta: testutil.ObjectMeta("stack0"),
		Status:     v1beta1.BrokerStatus{Status: v1beta1.Status{Ready: false}},
	})
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}

	_, err := GetTopicsEnvVars(ctx, stack, "TOPICS", "ledger")
	require.Error(t, err)
}
