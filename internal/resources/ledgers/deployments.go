package ledgers

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/applications"
	"github.com/formancehq/operator/v3/internal/resources/auths"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
	"github.com/formancehq/operator/v3/internal/resources/brokertopics"
	"github.com/formancehq/operator/v3/internal/resources/databases"
	"github.com/formancehq/operator/v3/internal/resources/gateways"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/services"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func installLedger(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration, version string) (err error) {
	if err := uninstallLedgerMonoWriterMultipleReader(ctx, stack); err != nil {
		return err
	}
	if err := installLedgerStateless(ctx, stack, ledger, database, imageConfiguration); err != nil {
		return err
	}
	if !semver.IsValid(version) || semver.Compare(version, "v2.3.0-alpha") > 0 {
		if err := installLedgerWorker(ctx, stack, ledger, database, imageConfiguration); err != nil {
			return err
		}
	}
	return nil
}

func installLedgerStateless(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration) error {
	container := corev1.Container{
		Name: "ledger",
	}
	container.Env = append(container.Env,
		core.Env("BIND", ":8080"),
	)

	experimentalFeatures, err := settings.PreferSpecBool(ctx, stack.Name, ledger.Spec.ExperimentalFeatures,
		"Ledger.Spec.ExperimentalFeatures", "ledger", "experimental-features")
	if err != nil {
		return fmt.Errorf("failed to get experimental features: %w", err)
	}
	if experimentalFeatures {
		container.Env = append(container.Env,
			core.Env("EXPERIMENTAL_FEATURES", "true"),
		)
	}

	experimentalNumscript, err := settings.PreferSpecBool(ctx, stack.Name, ledger.Spec.ExperimentalNumscript,
		"Ledger.Spec.ExperimentalNumscript", "ledger", "experimental-numscript")
	if err != nil {
		return fmt.Errorf("failed to get experimental numscript: %w", err)
	}
	if experimentalNumscript {
		container.Env = append(container.Env,
			core.Env("EXPERIMENTAL_NUMSCRIPT_INTERPRETER", "true"),
		)
	}

	experimentalNumscriptFlags, err := settings.PreferSpecStringSlice(ctx, stack.Name, ledger.Spec.ExperimentalNumscriptFlags,
		"Ledger.Spec.ExperimentalNumscriptFlags", "ledger", "experimental-numscript-flags")
	if err != nil {
		return fmt.Errorf("failed to get experimental numscript: %w", err)
	}
	if len(experimentalNumscriptFlags) > 0 {
		container.Env = append(container.Env, core.Env("EXPERIMENTAL_NUMSCRIPT_INTERPRETER_FLAGS", strings.Join(experimentalNumscriptFlags, " ")))
	}

	defaultPageSize, err := settings.PreferSpecInt(ctx, stack.Name, ledgerAPIField(ledger, func(a *v1beta1.LedgerAPIConfig) *int { return a.DefaultPageSize }),
		"Ledger.Spec.API.DefaultPageSize", "ledger", "api", "default-page-size")
	if err != nil {
		return fmt.Errorf("failed to get default page size: %w", err)
	}
	if defaultPageSize != nil {
		container.Env = append(container.Env,
			core.Env("DEFAULT_PAGE_SIZE", fmt.Sprint(*defaultPageSize)),
		)
	}

	maxPageSize, err := settings.PreferSpecInt(ctx, stack.Name, ledgerAPIField(ledger, func(a *v1beta1.LedgerAPIConfig) *int { return a.MaxPageSize }),
		"Ledger.Spec.API.MaxPageSize", "ledger", "api", "max-page-size")
	if err != nil {
		return fmt.Errorf("failed to get max page size: %w", err)
	}
	if maxPageSize != nil {
		container.Env = append(container.Env,
			core.Env("MAX_PAGE_SIZE", fmt.Sprint(*maxPageSize)),
		)
	}

	var broker *v1beta1.Broker
	if t, err := brokertopics.Find(ctx, stack, "ledger"); err != nil {
		return err
	} else if t != nil && t.Status.Ready {
		broker = &v1beta1.Broker{}
		if err := ctx.GetClient().Get(ctx, types.NamespacedName{
			Name: stack.Name,
		}, broker); err != nil {
			return err
		}
	}

	if broker != nil {
		if !broker.Status.Ready {
			return core.NewPendingError().WithMessage("broker not ready")
		}

		brokerEnvVar, err := brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, "ledger")
		if err != nil {
			return err
		}

		container.Env = append(container.Env, brokerEnvVar...)
		container.Env = append(container.Env, brokers.GetPublisherEnvVars(stack, broker, "ledger")...)
	}

	bulkMaxSize, err := settings.PreferSpecInt(ctx, stack.Name, ledgerAPIField(ledger, func(a *v1beta1.LedgerAPIConfig) *int { return a.BulkMaxSize }),
		"Ledger.Spec.API.BulkMaxSize", "ledger", "api", "bulk-max-size")
	if err != nil {
		return err
	}
	if bulkMaxSize != nil {
		container.Env = append(container.Env, core.Env("BULK_MAX_SIZE", fmt.Sprint(*bulkMaxSize)))
	}

	schemaEnforcementMode, err := settings.PreferSpecString(ctx, stack.Name, ledger.Spec.SchemaEnforcementMode,
		"Ledger.Spec.SchemaEnforcementMode", "ledger", "schema-enforcement-mode")
	if err != nil {
		return err
	}
	if schemaEnforcementMode != "" {
		container.Env = append(container.Env, core.Env("SCHEMA_ENFORCEMENT_MODE", schemaEnforcementMode))
	}

	err = setCommonAPIContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, &container)
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	exportersEnabled, err := settings.PreferSpecBool(ctx, stack.Name, ledger.Spec.ExperimentalExporters,
		"Ledger.Spec.ExperimentalExporters", "ledger", "experimental-exporters")
	if err != nil {
		return fmt.Errorf("failed to get experimental exporters setting: %w", err)
	}
	if exportersEnabled {
		container.Env = append(container.Env,
			core.Env("EXPERIMENTAL_EXPORTERS", "true"),
			core.Env("WORKER_GRPC_ADDRESS", fmt.Sprintf("ledger-worker.%s:8081", stack.Name)),
		)
	}

	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ledger",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ImagePullSecrets:   imageConfiguration.PullSecrets,
					Containers:         []corev1.Container{container},
					ServiceAccountName: serviceAccountName,
				},
			},
		},
	}

	return applications.
		New(ledger, tpl).
		Install(ctx)
}

type asyncBlockHasherConfiguration struct {
	MaxBlockSize string `json:"max-block-size,omitempty"`
	Schedule     string `json:"schedule,omitempty"`
}

type pipelinesConfiguration struct {
	PullInterval    string `json:"pull-interval,omitempty"`
	PushRetryPeriod string `json:"push-retry-period,omitempty"`
	SyncPeriod      string `json:"sync-period,omitempty"`
	LogsPageSize    string `json:"logs-page-size,omitempty"`
}

type bucketCleanupConfiguration struct {
	RetentionPeriod string `json:"retention-period,omitempty"`
	Schedule        string `json:"schedule,omitempty"`
}

func installLedgerWorker(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration) error {
	container := corev1.Container{
		Name: "ledger-worker",
		Args: []string{"worker"},
	}

	err := setCommonContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, &container)
	if err != nil {
		return err
	}

	// Async block hasher settings
	asyncBlockHasher, err := resolveAsyncBlockHasher(ctx, stack.Name, ledger)
	if err != nil {
		return err
	}
	if asyncBlockHasher.MaxBlockSize != "" {
		container.Env = append(container.Env, core.Env("WORKER_ASYNC_BLOCK_HASHER_MAX_BLOCK_SIZE", asyncBlockHasher.MaxBlockSize))
	}
	if asyncBlockHasher.Schedule != "" {
		container.Env = append(container.Env, core.Env("WORKER_ASYNC_BLOCK_HASHER_SCHEDULE", asyncBlockHasher.Schedule))
	}

	// Pipelines settings
	pipelines, err := resolvePipelines(ctx, stack.Name, ledger)
	if err != nil {
		return err
	}
	if pipelines.PullInterval != "" {
		container.Env = append(container.Env, core.Env("WORKER_PIPELINES_PULL_INTERVAL", pipelines.PullInterval))
	}
	if pipelines.PushRetryPeriod != "" {
		container.Env = append(container.Env, core.Env("WORKER_PIPELINES_PUSH_RETRY_PERIOD", pipelines.PushRetryPeriod))
	}
	if pipelines.SyncPeriod != "" {
		container.Env = append(container.Env, core.Env("WORKER_PIPELINES_SYNC_PERIOD", pipelines.SyncPeriod))
	}
	if pipelines.LogsPageSize != "" {
		container.Env = append(container.Env, core.Env("WORKER_PIPELINES_LOGS_PAGE_SIZE", pipelines.LogsPageSize))
	}

	// Schema enforcement mode
	schemaEnforcementMode, err := settings.PreferSpecString(ctx, stack.Name, ledger.Spec.SchemaEnforcementMode,
		"Ledger.Spec.SchemaEnforcementMode", "ledger", "schema-enforcement-mode")
	if err != nil {
		return err
	}
	if schemaEnforcementMode != "" {
		container.Env = append(container.Env, core.Env("SCHEMA_ENFORCEMENT_MODE", schemaEnforcementMode))
	}

	// Bucket cleanup settings
	bucketCleanup, err := resolveBucketCleanup(ctx, stack.Name, ledger)
	if err != nil {
		return err
	}
	if bucketCleanup.RetentionPeriod != "" {
		container.Env = append(container.Env, core.Env("WORKER_BUCKET_CLEANUP_RETENTION_PERIOD", bucketCleanup.RetentionPeriod))
	}
	if bucketCleanup.Schedule != "" {
		container.Env = append(container.Env, core.Env("WORKER_BUCKET_CLEANUP_SCHEDULE", bucketCleanup.Schedule))
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	exportersEnabled, err := settings.PreferSpecBool(ctx, stack.Name, ledger.Spec.ExperimentalExporters,
		"Ledger.Spec.ExperimentalExporters", "ledger", "experimental-exporters")
	if err != nil {
		return fmt.Errorf("failed to get experimental exporters setting: %w", err)
	}

	if exportersEnabled {
		container.Ports = []corev1.ContainerPort{{
			Name:          "grpc",
			ContainerPort: 8081,
		}}
	}

	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ledger-worker",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ImagePullSecrets:   imageConfiguration.PullSecrets,
					Containers:         []corev1.Container{container},
					ServiceAccountName: serviceAccountName,
				},
			},
		},
	}

	err = applications.
		New(ledger, tpl).
		Stateful().
		Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install ledger worker: %w", err)
	}

	if exportersEnabled {
		_, err := services.Create(ctx, ledger, "ledger-worker", services.WithConfig(services.PortConfig{
			ServiceName: "ledger-worker",
			PortName:    "grpc",
			Port:        8081,
			TargetPort:  "grpc",
		}))
		if err != nil {
			return err
		}
	}

	return nil
}

func uninstallLedgerMonoWriterMultipleReader(ctx core.Context, stack *v1beta1.Stack) error {

	remove := func(name string) error {
		if err := core.DeleteIfExists[*appsv1.Deployment](ctx, core.GetNamespacedResourceName(stack.Name, name)); err != nil {
			return err
		}
		if err := core.DeleteIfExists[*corev1.Service](ctx, core.GetNamespacedResourceName(stack.Name, name)); err != nil {
			return err
		}

		return nil
	}

	if err := remove("ledger-write"); err != nil {
		return err
	}

	if err := remove("ledger-read"); err != nil {
		return err
	}

	if err := core.DeleteIfExists[*appsv1.Deployment](ctx, core.GetNamespacedResourceName(stack.Name, "ledger-gateway")); err != nil {
		return err
	}

	return nil
}

func setCommonContainerConfiguration(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, imageConfiguration *registries.ImageConfiguration, database *v1beta1.Database, container *corev1.Container) error {

	env := make([]corev1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, core.LowerCamelCaseKind(ctx, ledger), " ")
	if err != nil {
		return err
	}
	env = append(env, otlpEnv...)
	env = append(env, core.GetDevEnvVars(stack, ledger)...)

	postgresEnvVar, err := databases.GetPostgresEnvVars(ctx, stack, database)
	if err != nil {
		return err
	}
	env = append(env, postgresEnvVar...)

	container.Image = imageConfiguration.GetFullImageName()
	container.Env = append(container.Env, env...)
	container.Env = append(container.Env, core.Env("STORAGE_POSTGRES_CONN_STRING", "$(POSTGRES_URI)"))
	container.Env = append(container.Env, core.Env("STORAGE_DRIVER", "postgres"))

	return nil
}

func setCommonAPIContainerConfiguration(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, imageConfiguration *registries.ImageConfiguration, database *v1beta1.Database, container *corev1.Container) error {

	if err := setCommonContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, container); err != nil {
		return err
	}

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "ledger", ledger.Spec.Auth)
	if err != nil {
		return err
	}
	container.Env = append(container.Env, authEnvVars...)

	gatewayEnv, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return err
	}
	container.Env = append(container.Env, gatewayEnv...)
	container.Ports = []corev1.ContainerPort{applications.StandardHTTPPort()}
	container.LivenessProbe = applications.DefaultLiveness("http")
	container.ReadinessProbe = applications.DefaultReadiness("http")

	return nil
}

// ledgerAPIField safely returns a field of Spec.API, or nil when API is unset.
func ledgerAPIField(ledger *v1beta1.Ledger, pick func(*v1beta1.LedgerAPIConfig) *int) *int {
	if ledger.Spec.API == nil {
		return nil
	}
	return pick(ledger.Spec.API)
}

func resolveAsyncBlockHasher(ctx core.Context, stack string, ledger *v1beta1.Ledger) (asyncBlockHasherConfiguration, error) {
	if ledger.Spec.Worker != nil && ledger.Spec.Worker.AsyncBlockHasher != nil {
		return asyncBlockHasherConfiguration{
			MaxBlockSize: ledger.Spec.Worker.AsyncBlockHasher.MaxBlockSize,
			Schedule:     ledger.Spec.Worker.AsyncBlockHasher.Schedule,
		}, nil
	}
	value, err := settings.GetAs[asyncBlockHasherConfiguration](ctx, stack, "ledger", "worker", "async-block-hasher")
	if err != nil {
		return asyncBlockHasherConfiguration{}, err
	}
	if value == nil {
		return asyncBlockHasherConfiguration{}, nil
	}
	if value.MaxBlockSize != "" || value.Schedule != "" {
		settings.LogDeprecation(ctx, stack, "Ledger.Spec.Worker.AsyncBlockHasher", "ledger", "worker", "async-block-hasher")
	}
	return *value, nil
}

func resolvePipelines(ctx core.Context, stack string, ledger *v1beta1.Ledger) (pipelinesConfiguration, error) {
	if ledger.Spec.Worker != nil && ledger.Spec.Worker.Pipelines != nil {
		return pipelinesConfiguration{
			PullInterval:    ledger.Spec.Worker.Pipelines.PullInterval,
			PushRetryPeriod: ledger.Spec.Worker.Pipelines.PushRetryPeriod,
			SyncPeriod:      ledger.Spec.Worker.Pipelines.SyncPeriod,
			LogsPageSize:    ledger.Spec.Worker.Pipelines.LogsPageSize,
		}, nil
	}
	value, err := settings.GetAs[pipelinesConfiguration](ctx, stack, "ledger", "worker", "pipelines")
	if err != nil {
		return pipelinesConfiguration{}, err
	}
	if value == nil {
		return pipelinesConfiguration{}, nil
	}
	if value.PullInterval != "" || value.PushRetryPeriod != "" || value.SyncPeriod != "" || value.LogsPageSize != "" {
		settings.LogDeprecation(ctx, stack, "Ledger.Spec.Worker.Pipelines", "ledger", "worker", "pipelines")
	}
	return *value, nil
}

func resolveBucketCleanup(ctx core.Context, stack string, ledger *v1beta1.Ledger) (bucketCleanupConfiguration, error) {
	if ledger.Spec.Worker != nil && ledger.Spec.Worker.BucketCleanup != nil {
		return bucketCleanupConfiguration{
			RetentionPeriod: ledger.Spec.Worker.BucketCleanup.RetentionPeriod,
			Schedule:        ledger.Spec.Worker.BucketCleanup.Schedule,
		}, nil
	}
	value, err := settings.GetAs[bucketCleanupConfiguration](ctx, stack, "ledger", "worker", "bucket-cleanup")
	if err != nil {
		return bucketCleanupConfiguration{}, err
	}
	if value == nil {
		return bucketCleanupConfiguration{}, nil
	}
	if value.RetentionPeriod != "" || value.Schedule != "" {
		settings.LogDeprecation(ctx, stack, "Ledger.Spec.Worker.BucketCleanup", "ledger", "worker", "bucket-cleanup")
	}
	return *value, nil
}
