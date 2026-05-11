package payments

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestGetEncryptionKeyUsesSettingsWhenSpecIsEmpty(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		settingspkg.New("encryption-key", "payments.encryption-key", "from-settings", "stack0"),
	)

	key, err := getEncryptionKey(ctx, &v1beta1.Payments{Spec: v1beta1.PaymentsSpec{
		StackDependency: v1beta1.StackDependency{Stack: "stack0"},
	}})
	require.NoError(t, err)
	require.Equal(t, "from-settings", key)
}

func TestGetEncryptionKeyReturnsEmptyWhenSpecIsSet(t *testing.T) {
	t.Parallel()

	key, err := getEncryptionKey(testutil.NewContext(), &v1beta1.Payments{Spec: v1beta1.PaymentsSpec{
		StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		EncryptionKey:   "from-spec",
	}})
	require.NoError(t, err)
	require.Empty(t, key)
}

func TestTemporalEnvVarsWithoutReferencedSecrets(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	payments := &v1beta1.Payments{
		ObjectMeta: testutil.ObjectMeta("payments0"),
		Spec: v1beta1.PaymentsSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
	ctx := testutil.NewContext(
		settingspkg.New("temporal-dsn", "temporal.dsn", "temporal://temporal.stack0:7233/payments?initSearchAttributes=true", "stack0"),
		settingspkg.New("temporal-crt", "temporal.tls.crt", "crt", "stack0"),
		settingspkg.New("temporal-key", "temporal.tls.key", "key", "stack0"),
		settingspkg.New("workflow-pollers", "payments.worker.temporal-max-concurrent-workflow-task-pollers", "8", "stack0"),
		settingspkg.New("activity-pollers", "payments.worker.temporal-max-concurrent-activity-task-pollers", "9", "stack0"),
		settingspkg.New("slots", "payments.worker.temporal-max-slots-per-poller", "12", "stack0"),
		settingspkg.New("local-slots", "payments.worker.temporal-max-local-activity-slots", "34", "stack0"),
	)

	hash, env, err := temporalEnvVars(ctx, stack, payments)
	require.NoError(t, err)
	require.Empty(t, hash)

	envMap := testutil.EnvMap(env)
	require.Equal(t, "temporal.stack0:7233", envMap["TEMPORAL_ADDRESS"])
	require.Equal(t, "payments", envMap["TEMPORAL_NAMESPACE"])
	require.Equal(t, "crt", envMap["TEMPORAL_SSL_CLIENT_CERT"])
	require.Equal(t, "key", envMap["TEMPORAL_SSL_CLIENT_KEY"])
	require.Equal(t, "true", envMap["TEMPORAL_INIT_SEARCH_ATTRIBUTES"])
	require.Equal(t, "8", envMap["TEMPORAL_MAX_CONCURRENT_WORKFLOW_TASK_POLLERS"])
	require.Equal(t, "9", envMap["TEMPORAL_MAX_CONCURRENT_ACTIVITY_TASK_POLLERS"])
	require.Equal(t, "12", envMap["TEMPORAL_MAX_SLOTS_PER_POLLER"])
	require.Equal(t, "34", envMap["TEMPORAL_MAX_LOCAL_ACTIVITY_SLOTS"])
}

func TestTemporalEnvVarsWithReferencedSecretWaitsForResourceReference(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	payments := &v1beta1.Payments{
		ObjectMeta: testutil.ObjectMeta("payments0"),
		Spec: v1beta1.PaymentsSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
	ctx := testutil.NewContext(
		settingspkg.New("temporal-dsn", "temporal.dsn", "temporal://temporal.stack0:7233/payments?secret=temporal-tls", "stack0"),
	)

	_, _, err := temporalEnvVars(ctx, stack, payments)
	require.True(t, core.IsApplicationError(err))
}

func TestTemporalEnvVarsWithReadyReferencedSecrets(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	payments := &v1beta1.Payments{
		TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Payments"},
		ObjectMeta: testutil.ObjectMeta("payments0"),
		Spec: v1beta1.PaymentsSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
	tlsRef := readyResourceReference("payments0-payments-temporal", "tls-hash")
	encryptionRef := readyResourceReference("payments0-payments-temporal-encryption-key", "encryption-hash")
	ctx := testutil.NewContext(
		payments,
		tlsRef,
		encryptionRef,
		settingspkg.New("temporal-dsn", "temporal.dsn", "temporal://temporal.stack0:7233/payments?secret=temporal-tls&encryptionKeySecret=temporal-encryption", "stack0"),
	)

	hash, env, err := temporalEnvVars(ctx, stack, payments)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"payments0-payments-temporal":                "tls-hash",
		"payments0-payments-temporal-encryption-key": "encryption-hash",
	}, hash)

	envByName := envVarsByName(env)
	require.Equal(t, "temporal.stack0:7233", envByName["TEMPORAL_ADDRESS"].Value)
	require.Equal(t, "payments", envByName["TEMPORAL_NAMESPACE"].Value)
	requireSecretEnv(t, envByName["TEMPORAL_SSL_CLIENT_KEY"], "temporal-tls", "tls.key")
	requireSecretEnv(t, envByName["TEMPORAL_SSL_CLIENT_CERT"], "temporal-tls", "tls.crt")
	require.Equal(t, "true", envByName["TEMPORAL_ENCRYPTION_ENABLED"].Value)
	requireSecretEnv(t, envByName["TEMPORAL_ENCRYPTION_KEY"], "temporal-encryption", "key")
}

func TestTemporalEnvVarsUsesWorkerDefaultsAndRejectsInvalidWorkerSettings(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	payments := &v1beta1.Payments{
		ObjectMeta: testutil.ObjectMeta("payments0"),
		Spec: v1beta1.PaymentsSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
	ctx := testutil.NewContext(
		settingspkg.New("temporal-dsn", "temporal.dsn", "temporal://temporal.stack0:7233/payments", "stack0"),
	)

	_, env, err := temporalEnvVars(ctx, stack, payments)
	require.NoError(t, err)
	envMap := testutil.EnvMap(env)
	require.Equal(t, "4", envMap["TEMPORAL_MAX_CONCURRENT_WORKFLOW_TASK_POLLERS"])
	require.Equal(t, "4", envMap["TEMPORAL_MAX_CONCURRENT_ACTIVITY_TASK_POLLERS"])
	require.Equal(t, "10", envMap["TEMPORAL_MAX_SLOTS_PER_POLLER"])
	require.Equal(t, "50", envMap["TEMPORAL_MAX_LOCAL_ACTIVITY_SLOTS"])

	invalidSettings := []string{
		"payments.worker.temporal-max-concurrent-workflow-task-pollers",
		"payments.worker.temporal-max-concurrent-activity-task-pollers",
		"payments.worker.temporal-max-slots-per-poller",
		"payments.worker.temporal-max-local-activity-slots",
	}
	for _, key := range invalidSettings {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			ctx := testutil.NewContext(
				settingspkg.New("temporal-dsn", "temporal.dsn", "temporal://temporal.stack0:7233/payments", "stack0"),
				settingspkg.New("invalid-worker-setting", key, "not-an-int", "stack0"),
			)
			_, _, err := temporalEnvVars(ctx, stack, payments)
			require.Error(t, err)
		})
	}
}

func TestCommonEnvVarsBuildsPostgresAndModuleEnv(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	payments := &v1beta1.Payments{
		ObjectMeta: testutil.ObjectMeta("payments0"),
		Spec: v1beta1.PaymentsSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			ModuleProperties: v1beta1.ModuleProperties{
				DevProperties: v1beta1.DevProperties{Debug: true, Dev: true},
			},
		},
	}
	database := &v1beta1.Database{
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "payments",
		},
		Status: v1beta1.DatabaseStatus{
			URI:      testutil.MustParseURI("postgresql://user:p%40ss@postgres.stack0:5432/payments?sslmode=disable"),
			Database: "payments",
		},
	}
	ctx := testutil.NewContext(
		settingspkg.New("encryption-key", "payments.encryption-key", "secret-key", "stack0"),
		settingspkg.New("connection-pool", "modules.payments.database.connection-pool", "max-idle=3,max-lifetime=10m", "stack0"),
	)

	env, err := commonEnvVars(ctx, stack, payments, database)
	require.NoError(t, err)

	envMap := testutil.EnvMap(env)
	require.Equal(t, "postgres.stack0", envMap["POSTGRES_HOST"])
	require.Equal(t, "5432", envMap["POSTGRES_PORT"])
	require.Equal(t, "payments", envMap["POSTGRES_DATABASE"])
	require.Equal(t, "secret-key", envMap["CONFIG_ENCRYPTION_KEY"])
	require.Equal(t, "true", envMap["DEBUG"])
	require.Equal(t, "true", envMap["DEV"])
	require.Equal(t, "3", envMap["POSTGRES_MAX_IDLE_CONNS"])
	require.Equal(t, "10m", envMap["POSTGRES_CONN_MAX_LIFETIME"])
}

func TestCreateV2ReadDeployment(t *testing.T) {
	t.Parallel()

	stack, payments, database := paymentsFixtures()
	image := &registries.ImageConfiguration{
		Registry: "ghcr.io",
		Image:    "formancehq/payments",
		Version:  "v2.0.0",
	}
	ctx := testutil.NewContext(
		payments,
		settingspkg.New("encryption-key", "payments.encryption-key", "secret-key", "stack0"),
	)

	require.NoError(t, createV2ReadDeployment(ctx, stack, payments, database, image))

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "payments-read", Namespace: "stack0"}, deployment))
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)

	container := deployment.Spec.Template.Spec.Containers[0]
	envMap := testutil.EnvMap(container.Env)
	require.Equal(t, "api", container.Name)
	require.Equal(t, []string{"api", "serve"}, container.Args)
	require.Equal(t, "ghcr.io/formancehq/payments:v2.0.0", container.Image)
	require.Equal(t, "secret-key", envMap["CONFIG_ENCRYPTION_KEY"])
	require.Equal(t, "$(POSTGRES_DATABASE)", envMap["POSTGRES_DATABASE_NAME"])
	require.NotNil(t, container.LivenessProbe)

	service := &corev1.Service{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "payments-read", Namespace: "stack0"}, service))
	require.Equal(t, int32(8080), service.Spec.Ports[0].Port)
	require.Equal(t, "payments-read", service.Labels["app.kubernetes.io/service-name"])
}

func TestCreateV2ConnectorsDeploymentAppliesBrokerAndServiceSettings(t *testing.T) {
	t.Parallel()

	stack, payments, database := paymentsFixtures()
	image := &registries.ImageConfiguration{
		Registry: "ghcr.io",
		Image:    "formancehq/payments",
		Version:  "v2.0.0",
	}
	topic := &v1beta1.BrokerTopic{
		ObjectMeta: testutil.ObjectMeta("payments-topic"),
		Spec: v1beta1.BrokerTopicSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "payments",
		},
		Status: v1beta1.BrokerTopicStatus{Status: v1beta1.Status{Ready: true}},
	}
	broker := &v1beta1.Broker{
		ObjectMeta: testutil.ObjectMeta("stack0"),
		Spec: v1beta1.BrokerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
		Status: v1beta1.BrokerStatus{
			Status: v1beta1.Status{Ready: true},
			URI:    testutil.MustParseURI("nats://nats.stack0:4222?circuitBreakerEnabled=true&circuitBreakerOpenInterval=5s"),
			Mode:   v1beta1.ModeOneStreamByStack,
		},
	}
	ctx := newPaymentsContextWithBrokerTopicIndexes(
		payments,
		topic,
		broker,
		settingspkg.New("encryption-key", "payments.encryption-key", "secret-key", "stack0"),
		settingspkg.New("aws-sa", "aws.service-account", "payments-connectors-aws", "stack0"),
		settingspkg.New("service-annotations", "services.payments-connectors.annotations", "owner=payments", "stack0"),
		settingspkg.New("service-traffic", "services.payments-connectors.traffic-distribution", "PreferClose", "stack0"),
	)

	require.NoError(t, createV2ConnectorsDeployment(ctx, stack, payments, database, image))

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "payments-connectors", Namespace: "stack0"}, deployment))
	require.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)
	require.Equal(t, "payments-connectors-aws", deployment.Spec.Template.Spec.ServiceAccountName)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	require.Equal(t, "connectors", container.Name)
	require.Equal(t, []string{"connectors", "serve"}, container.Args)
	envMap := testutil.EnvMap(container.Env)
	require.Equal(t, "secret-key", envMap["CONFIG_ENCRYPTION_KEY"])
	require.Equal(t, "nats", envMap["BROKER"])
	require.Equal(t, "true", envMap["PUBLISHER_NATS_ENABLED"])
	require.Equal(t, "nats.stack0:4222", envMap["PUBLISHER_NATS_URL"])
	require.Equal(t, "stack0-payments", envMap["PUBLISHER_NATS_CLIENT_ID"])
	require.Equal(t, "*:stack0.payments", envMap["PUBLISHER_TOPIC_MAPPING"])
	require.Equal(t, "false", envMap["PUBLISHER_NATS_AUTO_PROVISION"])
	require.Equal(t, "true", envMap["PUBLISHER_CIRCUIT_BREAKER_ENABLED"])
	require.Equal(t, "5s", envMap["PUBLISHER_CIRCUIT_BREAKER_OPEN_INTERVAL_DURATION"])

	service := &corev1.Service{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "payments-connectors", Namespace: "stack0"}, service))
	require.Equal(t, "payments", service.Annotations["owner"])
	require.NotNil(t, service.Spec.TrafficDistribution)
	require.Equal(t, "PreferClose", *service.Spec.TrafficDistribution)
}

func TestCreateGateway(t *testing.T) {
	t.Parallel()

	stack, payments, _ := paymentsFixtures()
	ctx := testutil.NewContext(
		payments,
		settingspkg.New("caddy-image", "caddy.image", "caddy:2.7.6-alpine", "stack0"),
	)

	require.NoError(t, createGateway(ctx, stack, payments))

	configMap := &corev1.ConfigMap{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "payments", Namespace: "stack0"}, configMap))
	require.Contains(t, configMap.Data["Caddyfile"], ":8080")

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "payments", Namespace: "stack0"}, deployment))
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	require.Empty(t, deployment.Spec.Template.Spec.InitContainers)
	require.Equal(t, "gateway", deployment.Spec.Template.Spec.Containers[0].Name)
	require.Equal(t, "docker.io/caddy:2.7.6-alpine", deployment.Spec.Template.Spec.Containers[0].Image)
	require.Equal(t, "payments", deployment.Spec.Template.Labels["app.kubernetes.io/name"])
}

func TestCreateV3DeploymentAppliesTemporalAndAWSSettings(t *testing.T) {
	t.Parallel()

	stack, payments, database := paymentsFixtures()
	image := &registries.ImageConfiguration{
		Registry: "ghcr.io",
		Image:    "formancehq/payments",
		Version:  "v3.0.0",
	}
	ctx := newPaymentsContextWithBrokerTopicIndexes(
		payments,
		settingspkg.New("temporal-dsn", "temporal.dsn", "temporal://temporal.stack0:7233/payments?initSearchAttributes=true", "stack0"),
		settingspkg.New("aws-sa", "aws.service-account", "payments-aws", "stack0"),
		settingspkg.New("workflow-pollers", "payments.worker.temporal-max-concurrent-workflow-task-pollers", "6", "stack0"),
		settingspkg.New("deployment-env", "deployments.payments-worker.containers.payments-worker.env-vars", "FROM_DEPLOYMENT_SETTINGS=true", "stack0"),
	)

	require.NoError(t, createV3Deployment(ctx, stack, payments, database, image, DeploymentTypeWorker))

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "payments-worker", Namespace: "stack0"}, deployment))
	require.Equal(t, "payments-aws", deployment.Spec.Template.Spec.ServiceAccountName)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	require.Equal(t, "payments-worker", container.Name)
	require.Equal(t, []string{"worker"}, container.Args)
	envMap := testutil.EnvMap(container.Env)
	require.Equal(t, "temporal.stack0:7233", envMap["TEMPORAL_ADDRESS"])
	require.Equal(t, "payments", envMap["TEMPORAL_NAMESPACE"])
	require.Equal(t, "true", envMap["TEMPORAL_INIT_SEARCH_ATTRIBUTES"])
	require.Equal(t, "6", envMap["TEMPORAL_MAX_CONCURRENT_WORKFLOW_TASK_POLLERS"])
	require.Equal(t, "true", envMap["FROM_DEPLOYMENT_SETTINGS"])
}

func TestValidateTemporalURI(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateTemporalURI(testutil.MustParseURI("temporal://temporal.stack0:7233/payments")))
	require.Error(t, validateTemporalURI(testutil.MustParseURI("http://temporal.stack0:7233/payments")))
	require.Error(t, validateTemporalURI(testutil.MustParseURI("temporal://temporal.stack0:7233")))
}

func readyResourceReference(name, hash string) *v1beta1.ResourceReference {
	return &v1beta1.ResourceReference{
		TypeMeta:   metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "ResourceReference"},
		ObjectMeta: testutil.ObjectMeta(name),
		Status: v1beta1.ResourceReferenceStatus{
			Status: v1beta1.Status{Ready: true},
			Hash:   hash,
		},
	}
}

func envVarsByName(env []corev1.EnvVar) map[string]corev1.EnvVar {
	ret := make(map[string]corev1.EnvVar, len(env))
	for _, item := range env {
		ret[item.Name] = item
	}
	return ret
}

func requireSecretEnv(t *testing.T, env corev1.EnvVar, secretName, key string) {
	t.Helper()

	require.NotNil(t, env.ValueFrom)
	require.NotNil(t, env.ValueFrom.SecretKeyRef)
	require.Equal(t, secretName, env.ValueFrom.SecretKeyRef.Name)
	require.Equal(t, key, env.ValueFrom.SecretKeyRef.Key)
}

func newPaymentsContextWithBrokerTopicIndexes(objects ...client.Object) *testutil.Context {
	scheme := testutil.NewScheme()
	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...)

	builder.WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
		return obj.(*v1beta1.Settings).GetStacks()
	})
	builder.WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
		keyParts := settingspkg.SplitKeywordWithDot(obj.(*v1beta1.Settings).Spec.Key)
		return []string{fmt.Sprintf("%d", len(keyParts))}
	})
	builder.WithIndex(&v1beta1.BrokerTopic{}, "stack", func(obj client.Object) []string {
		topic := obj.(*v1beta1.BrokerTopic)
		if topic.Spec.Stack == "" {
			return nil
		}
		return []string{topic.Spec.Stack}
	})
	builder.WithIndex(&v1beta1.BrokerTopic{}, ".spec.service", func(obj client.Object) []string {
		topic := obj.(*v1beta1.BrokerTopic)
		if topic.Spec.Service == "" {
			return nil
		}
		return []string{topic.Spec.Service}
	})
	withPaymentsUnstructuredStackIndex(builder, "Auth")
	withPaymentsUnstructuredStackIndex(builder, "Gateway")

	return &testutil.Context{
		Context: context.Background(),
		Client:  builder.Build(),
		Scheme:  scheme,
	}
}

func withPaymentsUnstructuredStackIndex(builder *fake.ClientBuilder, kind string) {
	object := &unstructured.Unstructured{}
	object.SetAPIVersion("formance.com/v1beta1")
	object.SetKind(kind)
	builder.WithIndex(object, "stack", func(obj client.Object) []string {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return nil
		}
		spec, ok := u.Object["spec"].(map[string]any)
		if !ok {
			return nil
		}
		stack, ok := spec["stack"].(string)
		if !ok || stack == "" {
			return nil
		}
		return []string{stack}
	})
}

func paymentsFixtures() (*v1beta1.Stack, *v1beta1.Payments, *v1beta1.Database) {
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	payments := &v1beta1.Payments{
		ObjectMeta: testutil.ObjectMeta("payments0"),
		Spec: v1beta1.PaymentsSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
	database := &v1beta1.Database{
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "payments",
		},
		Status: v1beta1.DatabaseStatus{
			URI:      testutil.MustParseURI("postgresql://payments:secret@postgres.stack0:5432/payments"),
			Database: "payments",
		},
	}
	return stack, payments, database
}
