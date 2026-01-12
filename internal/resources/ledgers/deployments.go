package ledgers

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/services"
	"github.com/formancehq/operator/internal/resources/settings"
)

func installLedger(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration, version string) (err error) {

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

	// For older versions, just use single instance deployment
	return installLedgerSingleInstance(ctx, stack, ledger, database, imageConfiguration)
}

func installLedgerSingleInstance(ctx core.Context, stack *v1beta1.Stack,
	ledger *v1beta1.Ledger, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration) error {
	container, err := createLedgerContainerFull(ctx, stack)
	if err != nil {
		return err
	}

	err = setCommonAPIContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, container)
	if err != nil {
		return err
	}

	if err := createDeployment(ctx, stack, ledger, "ledger", *container, 1, imageConfiguration); err != nil {
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

	experimentalNumscriptFlags, err := settings.GetStringSlice(ctx, stack.Name, "ledger", "experimental-numscript-flags")
	if err != nil {
		return fmt.Errorf("failed to get experimental numscript: %w", err)
	}
	if len(experimentalNumscriptFlags) > 0 {
		container.Env = append(container.Env, core.Env("EXPERIMENTAL_NUMSCRIPT_INTERPRETER_FLAGS", strings.Join(experimentalNumscriptFlags, " ")))
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
		container.Env = append(container.Env, brokers.GetPublisherEnvVars(stack, broker, "ledger")...)
	}

	bulkMaxSize, err := settings.GetInt(ctx, stack.Name, "ledger", "api", "bulk-max-size")
	if err != nil {
		return err
	}
	if bulkMaxSize != nil {
		container.Env = append(container.Env, core.Env("BULK_MAX_SIZE", fmt.Sprint(*bulkMaxSize)))
	}

	err = setCommonAPIContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, &container)
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

	err := setCommonContainerConfiguration(ctx, stack, ledger, imageConfiguration, database, &container)
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

func createDeployment(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, name string, container corev1.Container, replicas uint64, imageConfiguration *registries.ImageConfiguration) error {
	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	// No volumes needed for v2
	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
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
		WithStateful(replicas > 0).
		Install(ctx)
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

func createBaseLedgerContainer() *corev1.Container {
	ret := &corev1.Container{
		Name: "ledger",
	}
	ret.Env = append(ret.Env, core.Env("BIND", ":8080"))
	return ret
}

func createLedgerContainerFull(ctx core.Context, stack *v1beta1.Stack) (*corev1.Container, error) {
	container := createBaseLedgerContainer()

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

		brokerEnvVar, err := brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, "ledger")
		if err != nil {
			return nil, err
		}

		container.Env = append(container.Env, brokerEnvVar...)
		container.Env = append(container.Env, brokers.GetPublisherEnvVars(stack, broker, "ledger")...)
	}

	logsBatchSize, err := settings.GetInt(ctx, stack.Name, "ledger", "logs", "max-batch-size")
	if err != nil {
		return nil, err
	}
	if logsBatchSize != nil && *logsBatchSize != 0 {
		container.Env = append(container.Env, core.Env("LEDGER_BATCH_SIZE", fmt.Sprint(*logsBatchSize)))
	}

	return container, nil
}
