package ledgers

import (
	"fmt"
	"github.com/formancehq/operator/internal/resources/auths"
	"golang.org/x/mod/semver"

	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/jobs"
	"github.com/formancehq/operator/internal/resources/settings"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ConditionTypeRestore                 = "Restore"
	ConditionTypeReindexSchedulerEnabled = "ReindexSchedulerEnabled"
)

func installLedger(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, image string, version string) (err error) {

	if !semver.IsValid(version) || semver.Compare(version, "v2.2.0-alpha") > 0 {
		if err := uninstallLedgerMonoWriterMultipleReader(ctx, stack); err != nil {
			return err
		}
		if err := installLedgerStateless(ctx, stack, ledger, database, image); err != nil {
			return err
		}
		if !semver.IsValid(version) || semver.Compare(version, "v2.3.0-alpha") > 0 {
			if err := installLedgerWorker(ctx, stack, ledger, database, image); err != nil {
				return err
			}
		}
		return nil
	}

	// For older versions, just use single instance deployment
	return installLedgerSingleInstance(ctx, stack, ledger, database, image)
}

func installLedgerSingleInstance(ctx core.Context, stack *v1beta1.Stack,
	ledger *v1beta1.Ledger, database *v1beta1.Database, image string) error {
	container, err := createLedgerContainerFull(ctx, stack)
	if err != nil {
		return err
	}

	err = setCommonAPIContainerConfiguration(ctx, stack, ledger, image, database, container)
	if err != nil {
		return err
	}

	if err := createDeployment(ctx, stack, ledger, "ledger", *container, 1); err != nil {
		return err
	}

	return nil
}

func installLedgerStateless(ctx core.Context, stack *v1beta1.Stack,
	ledger *v1beta1.Ledger, database *v1beta1.Database, version string) error {
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

	err = setCommonAPIContainerConfiguration(ctx, stack, ledger, version, database, &container)
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ledger",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
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

func installLedgerWorker(ctx core.Context, stack *v1beta1.Stack,
	ledger *v1beta1.Ledger, database *v1beta1.Database, image string) error {
	container := corev1.Container{
		Name: "ledger-worker",
		Args: []string{"worker"},
	}

	err := setCommonContainerConfiguration(ctx, stack, ledger, image, database, &container)
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ledger-worker",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers:         []corev1.Container{container},
					ServiceAccountName: serviceAccountName,
				},
			},
		},
	}

	return applications.
		New(ledger, tpl).
		Stateful().
		Install(ctx)
}

func getUpgradeContainer(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database, image, version string) (corev1.Container, error) {
	return databases.MigrateDatabaseContainer(ctx, stack, image, database,
		func(m *databases.MigrationConfiguration) {
			if core.IsLower(version, "v2.0.0-rc.6") {
				m.Command = []string{"buckets", "upgrade-all"}
			}
			m.AdditionalEnv = []corev1.EnvVar{
				core.Env("STORAGE_POSTGRES_CONN_STRING", "$(POSTGRES_URI)"),
			}
		},
	)
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

func createDeployment(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, name string, container corev1.Container, replicas uint64) error {
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

func setCommonContainerConfiguration(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, image string, database *v1beta1.Database, container *corev1.Container) error {

	env := make([]corev1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVarsWithPrefix(ctx, stack.Name, core.LowerCamelCaseKind(ctx, ledger), "", " ")
	if err != nil {
		return err
	}
	env = append(env, otlpEnv...)
	env = append(env, core.GetDevEnvVarsWithPrefix(stack, ledger, "")...)

	postgresEnvVar, err := databases.PostgresEnvVarsWithPrefix(ctx, stack, database, "")
	if err != nil {
		return err
	}
	env = append(env, postgresEnvVar...)

	container.Image = image
	container.Env = append(container.Env, env...)
	container.Env = append(container.Env, core.Env("STORAGE_POSTGRES_CONN_STRING", "$(POSTGRES_URI)"))
	container.Env = append(container.Env, core.Env("STORAGE_DRIVER", "postgres"))

	return nil
}

func setCommonAPIContainerConfiguration(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, image string, database *v1beta1.Database, container *corev1.Container) error {

	if err := setCommonContainerConfiguration(ctx, stack, ledger, image, database, container); err != nil {
		return err
	}

	authEnvVars, err := auths.ProtectedAPIEnvVarsWithPrefix(ctx, stack, "ledger", ledger.Spec.Auth, "")
	if err != nil {
		return err
	}
	container.Env = append(container.Env, authEnvVars...)

	gatewayEnv, err := gateways.EnvVarsIfEnabledWithPrefix(ctx, stack.Name, "")
	if err != nil {
		return err
	}
	container.Env = append(container.Env, gatewayEnv...)
	container.Ports = []corev1.ContainerPort{applications.StandardHTTPPort()}
	container.LivenessProbe = applications.DefaultLiveness("http")

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

		brokerEnvVar, err := brokers.GetEnvVarsWithPrefix(ctx, broker.Status.URI, stack.Name, "ledger", "")
		if err != nil {
			return nil, err
		}

		container.Env = append(container.Env, brokerEnvVar...)
		container.Env = append(container.Env, brokers.GetPublisherEnvVars(stack, broker, "ledger", "")...)
	}

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

	return container, nil
}

func migrate(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, database *v1beta1.Database, image, version string) error {
	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	upgradeContainer, err := getUpgradeContainer(ctx, stack, database, image, version)
	if err != nil {
		return err
	}

	return jobs.Handle(ctx, ledger, "migrate-v2", upgradeContainer,
		jobs.PreCreate(func() error {
			list := &appsv1.DeploymentList{}
			if err := ctx.GetClient().List(ctx, list, client.InNamespace(stack.Name)); err != nil {
				return err
			}

			for _, item := range list.Items {
				if controller := metav1.GetControllerOf(&item); controller != nil && controller.UID == ledger.GetUID() {
					if err := ctx.GetClient().Delete(ctx, &item); err != nil {
						return err
					}
				}
			}
			return nil
		}),
		jobs.WithServiceAccount(serviceAccountName),
	)
}
