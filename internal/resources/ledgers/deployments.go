package ledgers

import (
	"fmt"
	"github.com/formancehq/operator/internal/resources/auths"
	"golang.org/x/mod/semver"
	"strconv"

	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/caddy"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/services"
	"github.com/formancehq/operator/internal/resources/settings"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeDeploymentStrategy      = "LedgerDeploymentStrategy"
	ReasonLedgerSingle                   = "Single"
	ReasonLedgerMonoWriterMultipleReader = "MonoWriterMultipleReader"
)

func hasDeploymentStrategyChanged(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, strategy string) (err error) {
	condition := v1beta1.NewCondition(ConditionTypeDeploymentStrategy, ledger.Generation).SetReason(
		func() string {
			switch strategy {
			case v1beta1.DeploymentStrategySingle:
				return ReasonLedgerSingle
			case v1beta1.DeploymentStrategyMonoWriterMultipleReader:
				return ReasonLedgerMonoWriterMultipleReader
			default:
				return "unknown strategy"
			}
		}(),
	).SetMessage("Deployment strategy initialized")

	defer func() {
		ledger.GetConditions().AppendOrReplace(*condition, v1beta1.AndConditions(
			v1beta1.ConditionTypeMatch(ConditionTypeDeploymentStrategy),
			v1beta1.ConditionGenerationMatch(ledger.Generation),
		))
	}()

	// There is no generation 0, so we can't check for a change in strategy
	// Uninstall is useless if the ledger deployment strategy has not changed
	if ledger.GetConditions().Check(v1beta1.AndConditions(
		v1beta1.ConditionTypeMatch(ConditionTypeDeploymentStrategy),
		v1beta1.ConditionReasonMatch(condition.Reason),
		v1beta1.ConditionGenerationMatch(ledger.Generation-1),
	)) || ledger.GetGeneration() == 1 {
		return
	}

	condition.SetMessage("Deployment strategy has changed")
	switch strategy {
	case v1beta1.DeploymentStrategySingle:
		return uninstallLedgerMonoWriterMultipleReader(ctx, stack)
	case v1beta1.DeploymentStrategyMonoWriterMultipleReader:
		return core.DeleteIfExists[*appsv1.Deployment](ctx, core.GetNamespacedResourceName(stack.Name, "ledger"))
	default:
		return fmt.Errorf("unknown deployment strategy %s", strategy)
	}
}

func installLedger(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration, version string, isV2 bool) (err error) {

	if !semver.IsValid(version) || semver.Compare(version, "v2.2.0-alpha") > 0 {
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

	deploymentStrategySettings, err := settings.GetStringOrDefault(ctx, stack.Name, v1beta1.DeploymentStrategySingle, "ledger", "deployment-strategy")
	if err != nil {
		return err
	}

	if ledger.Spec.DeploymentStrategy == v1beta1.DeploymentStrategyMonoWriterMultipleReader {
		deploymentStrategySettings = v1beta1.DeploymentStrategyMonoWriterMultipleReader
	}

	if err = hasDeploymentStrategyChanged(ctx, stack, ledger, deploymentStrategySettings); err != nil {
		return err
	}

	switch deploymentStrategySettings {
	case v1beta1.DeploymentStrategySingle:
		return installLedgerSingleInstance(ctx, stack, ledger, database, imageConfiguration, isV2)
	case v1beta1.DeploymentStrategyMonoWriterMultipleReader:
		return installLedgerMonoWriterMultipleReader(ctx, stack, ledger, database, imageConfiguration, isV2)
	default:
		return fmt.Errorf("unknown deployment strategy %s", deploymentStrategySettings)
	}
}

func installLedgerSingleInstance(
	ctx core.Context,
	stack *v1beta1.Stack,
	ledger *v1beta1.Ledger,
	database *v1beta1.Database,
	imageConfiguration *registries.ImageConfiguration,
	v2 bool,
) error {
	container, err := createLedgerContainerFull(ctx, stack, v2)
	if err != nil {
		return err
	}

	err = setCommonAPIContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, container, v2)
	if err != nil {
		return err
	}

	if !v2 && ledger.Spec.Locking != nil && ledger.Spec.Locking.Strategy == "redis" {
		container.Env = append(container.Env,
			core.Env("NUMARY_LOCK_STRATEGY", "redis"),
			core.Env("NUMARY_LOCK_STRATEGY_REDIS_URL", ledger.Spec.Locking.Redis.Uri),
			core.Env("NUMARY_LOCK_STRATEGY_REDIS_TLS_ENABLED", strconv.FormatBool(ledger.Spec.Locking.Redis.TLS)),
			core.Env("NUMARY_LOCK_STRATEGY_REDIS_TLS_INSECURE", strconv.FormatBool(ledger.Spec.Locking.Redis.InsecureTLS)),
		)

		if ledger.Spec.Locking.Redis.Duration != 0 {
			container.Env = append(container.Env, core.Env("NUMARY_LOCK_STRATEGY_REDIS_DURATION", ledger.Spec.Locking.Redis.Duration.String()))
		}

		if ledger.Spec.Locking.Redis.Retry != 0 {
			container.Env = append(container.Env, core.Env("NUMARY_LOCK_STRATEGY_REDIS_RETRY", ledger.Spec.Locking.Redis.Retry.String()))
		}
	}

	if err := createDeployment(ctx, stack, ledger, "ledger", *container, v2, 1, imageConfiguration); err != nil {
		return err
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

	experimentalFeatures, err := settings.GetBoolOrFalse(ctx, stack.Name, "ledger", "experimental-features")
	if err != nil {
		return fmt.Errorf("failed to get experimental features: %w", err)
	}
	if experimentalFeatures {
		container.Env = append(container.Env,
			core.Env("EXPERIMENTAL_FEATURES", "true"),
		)
	}

	experimentalNumscript, err := settings.GetBoolOrFalse(ctx, stack.Name, "ledger", "experimental-numscript")
	if err != nil {
		return fmt.Errorf("failed to get experimental numscript: %w", err)
	}
	if experimentalNumscript {
		container.Env = append(container.Env,
			core.Env("EXPERIMENTAL_NUMSCRIPT_INTERPRETER", "true"),
		)
	}

	defaultPageSize, err := settings.GetInt(ctx, stack.Name, "ledger", "api", "default-page-size")
	if err != nil {
		return fmt.Errorf("failed to get default page size: %w", err)
	}
	if defaultPageSize != nil {
		container.Env = append(container.Env,
			core.Env("DEFAULT_PAGE_SIZE", fmt.Sprint(*defaultPageSize)),
		)
	}

	maxPageSize, err := settings.GetInt(ctx, stack.Name, "ledger", "api", "max-page-size")
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
		container.Env = append(container.Env, brokers.GetPublisherEnvVars(stack, broker, "ledger", "")...)
	}

	bulkMaxSize, err := settings.GetInt(ctx, stack.Name, "ledger", "api", "bulk-max-size")
	if err != nil {
		return err
	}
	if bulkMaxSize != nil {
		container.Env = append(container.Env, core.Env("BULK_MAX_SIZE", fmt.Sprint(*bulkMaxSize)))
	}

	err = setCommonAPIContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, &container, true)
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	exportersEnabled, err := settings.GetBoolOrFalse(ctx, stack.Name, "ledger", "experimental-exporters")
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

func installLedgerWorker(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration) error {
	container := corev1.Container{
		Name: "ledger-worker",
		Args: []string{"worker"},
	}

	err := setCommonContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, &container, true)
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	exportersEnabled, err := settings.GetBoolOrFalse(ctx, stack.Name, "ledger", "experimental-exporters")
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

func installLedgerMonoWriterMultipleReader(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration, v2 bool) error {

	createDeployment := func(name string, container corev1.Container, replicas uint64) error {
		err := setCommonAPIContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, &container, v2)
		if err != nil {
			return err
		}

		if err := createDeployment(ctx, stack, ledger, name, container, v2, replicas, imageConfiguration); err != nil {
			return err
		}

		if _, err := services.Create(ctx, ledger, name, services.WithDefault(name)); err != nil {
			return err
		}

		return nil
	}

	container, err := createLedgerContainerWriteOnly(ctx, stack, v2)
	if err != nil {
		return err
	}
	if err := createDeployment("ledger-write", *container, 1); err != nil {
		return err
	}

	container = createLedgerContainerReadOnly(v2)
	if err := createDeployment("ledger-read", *container, 0); err != nil {
		return err
	}

	if err := createGatewayDeployment(ctx, stack, ledger); err != nil {
		return err
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

func createDeployment(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, name string, container corev1.Container, v2 bool, replicas uint64, imageConfiguration *registries.ImageConfiguration) error {
	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	var volumes []corev1.Volume
	if !v2 {
		volumes = []corev1.Volume{{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}}
	}

	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ImagePullSecrets:   imageConfiguration.PullSecrets,
					Containers:         []corev1.Container{container},
					Volumes:            volumes,
					ServiceAccountName: serviceAccountName,
				},
			},
		},
	}

	return applications.
		New(ledger, tpl).
		WithStateful(replicas > 0).
		Install(ctx)
}

func setCommonContainerConfiguration(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, imageConfiguration *registries.ImageConfiguration, database *v1beta1.Database, container *corev1.Container, v2 bool) error {

	prefix := ""
	if !v2 {
		prefix = "NUMARY_"
	}
	env := make([]corev1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVarsWithPrefix(ctx, stack.Name, core.LowerCamelCaseKind(ctx, ledger), prefix, " ")
	if err != nil {
		return err
	}
	env = append(env, otlpEnv...)
	env = append(env, core.GetDevEnvVarsWithPrefix(stack, ledger, prefix)...)

	postgresEnvVar, err := databases.PostgresEnvVarsWithPrefix(ctx, stack, database, prefix)
	if err != nil {
		return err
	}
	env = append(env, postgresEnvVar...)

	container.Image = imageConfiguration.GetFullImageName()
	container.Env = append(container.Env, env...)
	container.Env = append(container.Env, core.Env(fmt.Sprintf("%sSTORAGE_POSTGRES_CONN_STRING", prefix), fmt.Sprintf("$(%sPOSTGRES_URI)", prefix)))
	container.Env = append(container.Env, core.Env(fmt.Sprintf("%sSTORAGE_DRIVER", prefix), "postgres"))

	return nil
}

func setCommonAPIContainerConfiguration(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, imageConfiguration *registries.ImageConfiguration, database *v1beta1.Database, container *corev1.Container, v2 bool) error {

	prefix := ""
	if !v2 {
		prefix = "NUMARY_"
	}

	if err := setCommonContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, container, v2); err != nil {
		return err
	}

	authEnvVars, err := auths.ProtectedAPIEnvVarsWithPrefix(ctx, stack, "ledger", ledger.Spec.Auth, prefix)
	if err != nil {
		return err
	}
	container.Env = append(container.Env, authEnvVars...)

	gatewayEnv, err := gateways.EnvVarsIfEnabledWithPrefix(ctx, stack.Name, prefix)
	if err != nil {
		return err
	}
	container.Env = append(container.Env, gatewayEnv...)
	container.Ports = []corev1.ContainerPort{applications.StandardHTTPPort()}
	container.LivenessProbe = applications.DefaultLiveness("http")

	return nil
}

func createBaseLedgerContainer(v2 bool) *corev1.Container {
	ret := &corev1.Container{
		Name: "ledger",
	}
	var bindFlag = "BIND"
	if !v2 {
		bindFlag = "NUMARY_SERVER_HTTP_BIND_ADDRESS"
	}
	ret.Env = append(ret.Env, core.Env(bindFlag, ":8080"))
	if !v2 {
		ret.VolumeMounts = []corev1.VolumeMount{{
			Name:      "config",
			ReadOnly:  false,
			MountPath: "/root/.numary",
		}}
	}

	return ret
}

func createLedgerContainerFull(ctx core.Context, stack *v1beta1.Stack, v2 bool) (*corev1.Container, error) {
	container := createBaseLedgerContainer(v2)

	var broker *v1beta1.Broker
	if t, err := brokertopics.Find(ctx, stack, "ledger"); err != nil {
		return nil, err
	} else if t != nil && t.Status.Ready {
		broker = &v1beta1.Broker{}
		if err := ctx.GetClient().Get(ctx, types.NamespacedName{
			Name: stack.Name,
		}, broker); err != nil {
			return nil, err
		}
	}

	if broker != nil {
		if !broker.Status.Ready {
			return nil, core.NewPendingError().WithMessage("broker not ready")
		}
		prefix := ""
		if !v2 {
			prefix = "NUMARY_"
		}

		brokerEnvVar, err := brokers.GetEnvVarsWithPrefix(ctx, broker.Status.URI, stack.Name, "ledger", prefix)
		if err != nil {
			return nil, err
		}

		container.Env = append(container.Env, brokerEnvVar...)
		container.Env = append(container.Env, brokers.GetPublisherEnvVars(stack, broker, "ledger", prefix)...)
	}

	if v2 {
		hasDependency, err := core.HasDependency(ctx, stack.Name, &v1beta1.Analytics{})
		if err != nil {
			return nil, err
		}
		if hasDependency {
			container.Env = append(container.Env, core.Env("EMIT_LOGS", "true"))
		}

		logsBatchSize, err := settings.GetInt(ctx, stack.Name, "ledger", "logs", "max-batch-size")
		if err != nil {
			return nil, err
		}
		if logsBatchSize != nil && *logsBatchSize != 0 {
			container.Env = append(container.Env, core.Env("LEDGER_BATCH_SIZE", fmt.Sprint(*logsBatchSize)))
		}
	}

	return container, nil
}

func createLedgerContainerWriteOnly(ctx core.Context, stack *v1beta1.Stack, v2 bool) (*corev1.Container, error) {
	return createLedgerContainerFull(ctx, stack, v2)
}

func createLedgerContainerReadOnly(v2 bool) *corev1.Container {
	container := createBaseLedgerContainer(v2)
	container.Env = append(container.Env, core.Env("READ_ONLY", "true"))
	return container
}

func createGatewayDeployment(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger) error {

	caddyfileConfigMap, err := caddy.CreateCaddyfileConfigMap(ctx, stack, "ledger", Caddyfile, map[string]any{
		"Debug": stack.Spec.Debug || ledger.Spec.Debug,
	}, core.WithController[*corev1.ConfigMap](ctx.GetScheme(), ledger))
	if err != nil {
		return err
	}

	env := make([]corev1.EnvVar, 0)
	env = append(env, core.GetDevEnvVars(stack, ledger)...)

	caddyImage, err := registries.GetCaddyImage(ctx, stack)
	if err != nil {
		return err
	}

	tpl, err := caddy.DeploymentTemplate(ctx, stack, ledger, caddyfileConfigMap, caddyImage, env)
	if err != nil {
		return err
	}

	tpl.Name = "ledger-gateway"
	return applications.
		New(ledger, tpl).
		Install(ctx)
}
