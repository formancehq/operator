package ledgers

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
	"github.com/formancehq/operator/v3/internal/resources/registries"
	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestSetCommonAPIContainerConfiguration(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	ledger := &v1beta1.Ledger{
		Spec: v1beta1.LedgerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			ModuleProperties: v1beta1.ModuleProperties{
				DevProperties: v1beta1.DevProperties{Debug: true, Dev: true},
			},
		},
	}
	database := &v1beta1.Database{
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "ledger",
		},
		Status: v1beta1.DatabaseStatus{
			URI:      testutil.MustParseURI("postgresql://ledger:secret@postgres.stack0:5432/ledger"),
			Database: "ledger",
		},
	}
	image := &registries.ImageConfiguration{
		Registry: "ghcr.io",
		Image:    "formancehq/ledger",
		Version:  "v1.0.0",
	}
	ctx := testutil.NewContext(
		settingspkg.New("connection-pool", "modules.ledger.database.connection-pool", "max-idle=4", "stack0"),
	)
	container := createBaseLedgerContainer()

	require.NoError(t, setCommonAPIContainerConfiguration(ctx, stack, ledger, image, database, container))

	envMap := testutil.EnvMap(container.Env)
	require.Equal(t, "ghcr.io/formancehq/ledger:v1.0.0", container.Image)
	require.Equal(t, ":8080", envMap["BIND"])
	require.Equal(t, "true", envMap["DEBUG"])
	require.Equal(t, "true", envMap["DEV"])
	require.Equal(t, "$(POSTGRES_URI)", envMap["STORAGE_POSTGRES_CONN_STRING"])
	require.Equal(t, "postgres", envMap["STORAGE_DRIVER"])
	require.Equal(t, "4", envMap["POSTGRES_MAX_IDLE_CONNS"])
	require.NotNil(t, container.LivenessProbe)
	require.NotNil(t, container.ReadinessProbe)
	require.Len(t, container.Ports, 1)
}

func TestInstallLedgerStatelessAppliesAPISettingsToFinalDeployment(t *testing.T) {
	t.Parallel()

	stack, ledger, database, image := ledgerSettingsFixtures()
	ctx := newLedgerContextWithDependencyIndexes(
		ledger,
		settingspkg.New("experimental-features", "ledger.experimental-features", "true", "stack0"),
		settingspkg.New("experimental-numscript", "ledger.experimental-numscript", "true", "stack0"),
		settingspkg.New("experimental-numscript-flags", "ledger.experimental-numscript-flags", "flag-a,flag-b", "stack0"),
		settingspkg.New("default-page-size", "ledger.api.default-page-size", "25", "stack0"),
		settingspkg.New("max-page-size", "ledger.api.max-page-size", "100", "stack0"),
		settingspkg.New("bulk-max-size", "ledger.api.bulk-max-size", "500", "stack0"),
		settingspkg.New("schema", "ledger.schema-enforcement-mode", "strict", "stack0"),
		settingspkg.New("exporters", "ledger.experimental-exporters", "true", "stack0"),
		settingspkg.New("aws", "aws.service-account", "ledger-aws", "stack0"),
	)

	require.NoError(t, installLedgerStateless(ctx, stack, ledger, database, image))

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "ledger", Namespace: "stack0"}, deployment))
	require.Equal(t, "ledger-aws", deployment.Spec.Template.Spec.ServiceAccountName)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	envMap := testutil.EnvMap(container.Env)
	require.Equal(t, "true", envMap["EXPERIMENTAL_FEATURES"])
	require.Equal(t, "true", envMap["EXPERIMENTAL_NUMSCRIPT_INTERPRETER"])
	require.Equal(t, "flag-a flag-b", envMap["EXPERIMENTAL_NUMSCRIPT_INTERPRETER_FLAGS"])
	require.Equal(t, "25", envMap["DEFAULT_PAGE_SIZE"])
	require.Equal(t, "100", envMap["MAX_PAGE_SIZE"])
	require.Equal(t, "500", envMap["BULK_MAX_SIZE"])
	require.Equal(t, "strict", envMap["SCHEMA_ENFORCEMENT_MODE"])
	require.Equal(t, "true", envMap["EXPERIMENTAL_EXPORTERS"])
	require.Equal(t, "ledger-worker.stack0:8081", envMap["WORKER_GRPC_ADDRESS"])
	require.NotNil(t, container.LivenessProbe)
	require.NotNil(t, container.ReadinessProbe)
	require.Len(t, container.Ports, 1)
}

func TestInstallLedgerWorkerAppliesWorkerSettingsToFinalDeploymentAndService(t *testing.T) {
	t.Parallel()

	stack, ledger, database, image := ledgerSettingsFixtures()
	ctx := testutil.NewContext(
		ledger,
		settingspkg.New("async-block-hasher", "ledger.worker.async-block-hasher", "max-block-size=1000,schedule=*/5 * * * *", "stack0"),
		settingspkg.New("pipelines", "ledger.worker.pipelines", "pull-interval=1s,push-retry-period=2s,sync-period=3s,logs-page-size=42", "stack0"),
		settingspkg.New("bucket-cleanup", "ledger.worker.bucket-cleanup", "retention-period=720h,schedule=0 0 * * *", "stack0"),
		settingspkg.New("schema", "ledger.schema-enforcement-mode", "strict", "stack0"),
		settingspkg.New("exporters", "ledger.experimental-exporters", "true", "stack0"),
		settingspkg.New("aws", "aws.service-account", "ledger-worker-aws", "stack0"),
	)

	require.NoError(t, installLedgerWorker(ctx, stack, ledger, database, image))

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "ledger-worker", Namespace: "stack0"}, deployment))
	require.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)
	require.Equal(t, "ledger-worker-aws", deployment.Spec.Template.Spec.ServiceAccountName)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	require.Equal(t, []string{"worker"}, container.Args)
	envMap := testutil.EnvMap(container.Env)
	require.Equal(t, "1000", envMap["WORKER_ASYNC_BLOCK_HASHER_MAX_BLOCK_SIZE"])
	require.Equal(t, "*/5 * * * *", envMap["WORKER_ASYNC_BLOCK_HASHER_SCHEDULE"])
	require.Equal(t, "1s", envMap["WORKER_PIPELINES_PULL_INTERVAL"])
	require.Equal(t, "2s", envMap["WORKER_PIPELINES_PUSH_RETRY_PERIOD"])
	require.Equal(t, "3s", envMap["WORKER_PIPELINES_SYNC_PERIOD"])
	require.Equal(t, "42", envMap["WORKER_PIPELINES_LOGS_PAGE_SIZE"])
	require.Equal(t, "720h", envMap["WORKER_BUCKET_CLEANUP_RETENTION_PERIOD"])
	require.Equal(t, "0 0 * * *", envMap["WORKER_BUCKET_CLEANUP_SCHEDULE"])
	require.Equal(t, "strict", envMap["SCHEMA_ENFORCEMENT_MODE"])
	require.Len(t, container.Ports, 1)
	require.Equal(t, int32(8081), container.Ports[0].ContainerPort)

	service := &corev1.Service{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "ledger-worker", Namespace: "stack0"}, service))
	require.Len(t, service.Spec.Ports, 1)
	require.Equal(t, int32(8081), service.Spec.Ports[0].Port)
	require.Equal(t, "grpc", service.Spec.Ports[0].TargetPort.String())
}

func TestLedgerSettingsRejectInvalidValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		setting client.Object
		run     func(ctx *testutil.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, image *registries.ImageConfiguration) error
	}{
		{
			name:    "invalid api page size",
			setting: settingspkg.New("invalid-page-size", "ledger.api.default-page-size", "not-an-int", "stack0"),
			run: func(ctx *testutil.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, image *registries.ImageConfiguration) error {
				return installLedgerStateless(ctx, stack, ledger, database, image)
			},
		},
		{
			name:    "invalid worker structured setting",
			setting: settingspkg.New("invalid-pipelines", "ledger.worker.pipelines", `pull-interval="unterminated`, "stack0"),
			run: func(ctx *testutil.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, image *registries.ImageConfiguration) error {
				return installLedgerWorker(ctx, stack, ledger, database, image)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stack, ledger, database, image := ledgerSettingsFixtures()
			ctx := newLedgerContextWithDependencyIndexes(ledger, tc.setting)

			require.Error(t, tc.run(ctx, stack, ledger, database, image))
		})
	}
}

func ledgerSettingsFixtures() (*v1beta1.Stack, *v1beta1.Ledger, *v1beta1.Database, *registries.ImageConfiguration) {
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	ledger := &v1beta1.Ledger{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Ledger"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "ledger",
			UID:  types.UID("ledger-uid"),
		},
		Spec: v1beta1.LedgerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
	database := &v1beta1.Database{
		Spec: v1beta1.DatabaseSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Service:         "ledger",
		},
		Status: v1beta1.DatabaseStatus{
			URI:      testutil.MustParseURI("postgresql://ledger:secret@postgres.stack0:5432/ledger"),
			Database: "ledger",
		},
	}
	image := &registries.ImageConfiguration{
		Registry: "ghcr.io",
		Image:    "formancehq/ledger",
		Version:  "v2.0.0",
	}
	return stack, ledger, database, image
}

func newLedgerContextWithDependencyIndexes(objects ...client.Object) *testutil.Context {
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
	withLedgerUnstructuredStackIndex(builder, "Auth")
	withLedgerUnstructuredStackIndex(builder, "Gateway")

	return &testutil.Context{
		Context: context.Background(),
		Client:  builder.Build(),
		Scheme:  scheme,
	}
}

func withLedgerUnstructuredStackIndex(builder *fake.ClientBuilder, kind string) {
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
