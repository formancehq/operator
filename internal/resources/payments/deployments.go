package payments

import (
	"fmt"
	"strings"

	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/caddy"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/resourcereferences"
	"github.com/formancehq/operator/internal/resources/settings"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/services"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
)

func getEncryptionKey(ctx core.Context, payments *v1beta1.Payments) (string, error) {
	encryptionKey := payments.Spec.EncryptionKey
	if encryptionKey == "" {
		return settings.GetStringOrEmpty(ctx, payments.Spec.Stack, "payments", "encryption-key")
	}
	return "", nil
}

func temporalEnvVars(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments) ([]v1.EnvVar, error) {
	temporalURI, err := settings.RequireURL(ctx, stack.Name, "temporal", "dsn")
	if err != nil {
		return nil, err
	}

	if err := validateTemporalURI(temporalURI); err != nil {
		return nil, err
	}

	if secret := temporalURI.Query().Get("secret"); secret != "" {
		_, err = resourcereferences.Create(ctx, payments, "payments-temporal", secret, &v1.Secret{})
	} else {
		err = resourcereferences.Delete(ctx, payments, "payments-temporal")
	}
	if err != nil {
		return nil, err
	}

	if secret := temporalURI.Query().Get("encryptionKeySecret"); secret != "" {
		_, err = resourcereferences.Create(ctx, payments, "payments-temporal-encryption-key", secret, &v1.Secret{})
	} else {
		err = resourcereferences.Delete(ctx, payments, "payments-temporal-encryption-key")
	}
	if err != nil {
		return nil, err
	}

	env := make([]v1.EnvVar, 0)
	env = append(env,
		core.Env("TEMPORAL_ADDRESS", temporalURI.Host),
		core.Env("TEMPORAL_NAMESPACE", temporalURI.Path[1:]),
	)

	if secret := temporalURI.Query().Get("secret"); secret == "" {
		temporalTLSCrt, err := settings.GetStringOrEmpty(ctx, stack.Name, "temporal", "tls", "crt")
		if err != nil {
			return nil, err
		}

		temporalTLSKey, err := settings.GetStringOrEmpty(ctx, stack.Name, "temporal", "tls", "key")
		if err != nil {
			return nil, err
		}

		env = append(env,
			core.Env("TEMPORAL_SSL_CLIENT_KEY", temporalTLSKey),
			core.Env("TEMPORAL_SSL_CLIENT_CERT", temporalTLSCrt),
		)
	} else {
		env = append(env,
			core.EnvFromSecret("TEMPORAL_SSL_CLIENT_KEY", secret, "tls.key"),
			core.EnvFromSecret("TEMPORAL_SSL_CLIENT_CERT", secret, "tls.crt"),
		)
	}

	if secret := temporalURI.Query().Get("encryptionKeySecret"); secret != "" {
		env = append(env,
			core.Env("TEMPORAL_ENCRYPTION_ENABLED", "true"),
			core.EnvFromSecret("TEMPORAL_ENCRYPTION_KEY", secret, "key"),
		)
	}

	if initSearchAttributes := temporalURI.Query().Get("initSearchAttributes"); initSearchAttributes == "true" {
		env = append(env, core.Env("TEMPORAL_INIT_SEARCH_ATTRIBUTES", "true"))
	}

	temporalMaxConcurrentWorkflowTaskPollers, err := settings.GetIntOrDefault(ctx, stack.Name, 4, "payments", "worker", "temporal-max-concurrent-workflow-task-pollers")
	if err != nil {
		return nil, err
	}

	temporalMaxConcurrentActivityTaskPollers, err := settings.GetIntOrDefault(ctx, stack.Name, 4, "payments", "worker", "temporal-max-concurrent-activity-task-pollers")
	if err != nil {
		return nil, err
	}

	temporalMaxSlotsPerPoller, err := settings.GetIntOrDefault(ctx, stack.Name, 10, "payments", "worker", "temporal-max-slots-per-poller")
	if err != nil {
		return nil, err
	}

	temporalMaxLocalActivitySlots, err := settings.GetIntOrDefault(ctx, stack.Name, 50, "payments", "worker", "temporal-max-local-activity-slots")
	if err != nil {
		return nil, err
	}

	env = append(env,
		core.Env("TEMPORAL_MAX_CONCURRENT_WORKFLOW_TASK_POLLERS", fmt.Sprintf("%d", temporalMaxConcurrentWorkflowTaskPollers)),
		core.Env("TEMPORAL_MAX_CONCURRENT_ACTIVITY_TASK_POLLERS", fmt.Sprintf("%d", temporalMaxConcurrentActivityTaskPollers)),
		core.Env("TEMPORAL_MAX_SLOTS_PER_POLLER", fmt.Sprintf("%d", temporalMaxSlotsPerPoller)),
		core.Env("TEMPORAL_MAX_LOCAL_ACTIVITY_SLOTS", fmt.Sprintf("%d", temporalMaxLocalActivitySlots)),
	)

	return env, nil
}

func commonEnvVars(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments, database *v1beta1.Database) ([]v1.EnvVar, error) {
	env := make([]v1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, core.LowerCamelCaseKind(ctx, payments), " ")
	if err != nil {
		return nil, err
	}
	env = append(env, otlpEnv...)

	gatewayEnv, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return nil, err
	}

	postgresEnvVar, err := databases.GetPostgresEnvVars(ctx, stack, database)
	if err != nil {
		return nil, err
	}

	env = append(env, gatewayEnv...)
	env = append(env, core.GetDevEnvVars(stack, payments)...)
	env = append(env, postgresEnvVar...)

	encryptionKey, err := getEncryptionKey(ctx, payments)
	if err != nil {
		return nil, err
	}
	env = append(env,
		core.Env("POSTGRES_DATABASE_NAME", "$(POSTGRES_DATABASE)"),
		core.Env("CONFIG_ENCRYPTION_KEY", encryptionKey),
	)

	return env, nil
}

func uninstallPaymentsReadAndConnectors(ctx core.Context, stack *v1beta1.Stack) error {
	remove := func(name string) error {
		if err := core.DeleteIfExists[*appsv1.Deployment](ctx, core.GetNamespacedResourceName(stack.Name, name)); err != nil {
			return err
		}
		if err := core.DeleteIfExists[*corev1.Service](ctx, core.GetNamespacedResourceName(stack.Name, name)); err != nil {
			return err
		}

		return nil
	}

	if err := remove("payments-read"); err != nil {
		return err
	}

	if err := remove("payments-connectors"); err != nil {
		return err
	}

	return nil
}

func createFullDeployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	payments *v1beta1.Payments,
	database *v1beta1.Database,
	imageConfiguration *registries.ImageConfiguration,
	v3 bool,
) error {

	env, err := commonEnvVars(ctx, stack, payments, database)
	if err != nil {
		return err
	}

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "payments", payments.Spec.Auth)
	if err != nil {
		return err
	}
	env = append(env, authEnvVars...)

	var broker *v1beta1.Broker
	if t, err := brokertopics.Find(ctx, stack, "payments"); err != nil {
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
		brokerEnvVar, err := brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, "payments")
		if err != nil {
			return err
		}

		env = append(env, brokerEnvVar...)
		env = append(env, brokers.GetPublisherEnvVars(stack, broker, "payments", "")...)
	}

	if v3 {
		temporalEnvVars, err := temporalEnvVars(ctx, stack, payments)
		if err != nil {
			return err
		}

		env = append(env, temporalEnvVars...)
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	containerName := "api"
	appOpts := applications.WithProbePath("/_health")
	if v3 {
		containerName = "payments-api"
		appOpts = applications.WithProbePath("/_healthcheck")

		err := createWorkerDeployment(ctx, stack, payments, database, imageConfiguration, env, appOpts)
		if err != nil {
			return err
		}
	}

	err = applications.
		New(payments, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "payments",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []v1.Container{{
							Name:          containerName,
							Args:          []string{"serve"},
							Env:           env,
							Image:         imageConfiguration.GetFullImageName(),
							LivenessProbe: applications.DefaultLiveness("http", appOpts),
							Ports:         []v1.ContainerPort{applications.StandardHTTPPort()},
						}},
						// Ensure empty
						InitContainers: []v1.Container{},
					},
				},
			},
		}).
		Install(ctx)
	if err != nil {
		return err
	}

	return nil
}

func createWorkerDeployment(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration, env []v1.EnvVar, appOpts applications.ProbeOpts, ) error {
	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	err = applications.
		New(payments, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "payments-worker",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []v1.Container{{
							Name:          "payments-worker",
							Args:          []string{"worker"},
							Env:           env,
							Image:         imageConfiguration.GetFullImageName(),
							LivenessProbe: applications.DefaultLiveness("http", appOpts),
							Ports:         []v1.ContainerPort{applications.StandardHTTPPort()},
						}},
						// Ensure empty
						InitContainers: []v1.Container{},
					},
				},
			},
		}).
		Install(ctx)
	if err != nil {
		return err
	}

	return nil
}

func createReadDeployment(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration) error {

	env, err := commonEnvVars(ctx, stack, payments, database)
	if err != nil {
		return err
	}

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "payments", payments.Spec.Auth)
	if err != nil {
		return err
	}
	env = append(env, authEnvVars...)

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	err = applications.
		New(payments, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "payments-read",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []v1.Container{{
							Name:          "api",
							Args:          []string{"api", "serve"},
							Env:           env,
							Image:         imageConfiguration.GetFullImageName(),
							LivenessProbe: applications.DefaultLiveness("http", applications.WithProbePath("/_health")),
							Ports:         []v1.ContainerPort{applications.StandardHTTPPort()},
						}},
						// Ensure empty
						InitContainers: []v1.Container{},
					},
				},
			},
		}).
		Install(ctx)
	if err != nil {
		return err
	}

	_, err = services.Create(ctx, payments, "payments-read", services.WithDefault("payments-read"))
	if err != nil {
		return err
	}

	return nil
}

func createConnectorsDeployment(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration) error {

	env, err := commonEnvVars(ctx, stack, payments, database)
	if err != nil {
		return err
	}

	var broker *v1beta1.Broker
	if t, err := brokertopics.Find(ctx, stack, "payments"); err != nil {
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
		brokerEnvVar, err := brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, "payments")
		if err != nil {
			return err
		}

		env = append(env, brokerEnvVar...)
		env = append(env, brokers.GetPublisherEnvVars(stack, broker, "payments", "")...)
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	err = applications.
		New(payments, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "payments-connectors",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []v1.Container{{
							Name:  "connectors",
							Args:  []string{"connectors", "serve"},
							Env:   env,
							Image: imageConfiguration.GetFullImageName(),
							Ports: []v1.ContainerPort{applications.StandardHTTPPort()},
							LivenessProbe: applications.DefaultLiveness("http",
								applications.WithProbePath("/_health")),
						}},
						// Ensure empty
						InitContainers: []v1.Container{},
					},
				},
			},
		}).
		WithStateful(true).
		Install(ctx)
	if err != nil {
		return err
	}

	_, err = services.Create(ctx, payments, "payments-connectors", services.WithDefault("payments-connectors"))
	if err != nil {
		return err
	}

	return err
}

func createGateway(ctx core.Context, stack *v1beta1.Stack, p *v1beta1.Payments) error {

	caddyfileConfigMap, err := caddy.CreateCaddyfileConfigMap(ctx, stack, "payments", Caddyfile, map[string]any{
		"Debug": stack.Spec.Debug || p.Spec.Debug,
	}, core.WithController[*v1.ConfigMap](ctx.GetScheme(), p))
	if err != nil {
		return err
	}

	env := make([]v1.EnvVar, 0)

	env = append(env, core.GetDevEnvVars(stack, p)...)

	caddyImage, err := registries.GetCaddyImage(ctx, stack)
	if err != nil {
		return err
	}

	deploymentTemplate, err := caddy.DeploymentTemplate(ctx, stack, p, caddyfileConfigMap, caddyImage, env)
	if err != nil {
		return err
	}
	// notes(gfyrag): reset init containers in case of upgrading from v1 to v2
	deploymentTemplate.Spec.Template.Spec.InitContainers = make([]v1.Container, 0)

	deploymentTemplate.Name = "payments"

	return applications.
		New(p, deploymentTemplate).
		Install(ctx)
}

func validateTemporalURI(temporalURI *v1beta1.URI) error {
	if temporalURI.Scheme != "temporal" {
		return fmt.Errorf("invalid temporal uri: %s", temporalURI.String())
	}

	if temporalURI.Path == "" {
		return fmt.Errorf("invalid temporal uri: %s", temporalURI.String())
	}

	if !strings.HasPrefix(temporalURI.Path, "/") {
		return fmt.Errorf("invalid temporal uri: %s", temporalURI.String())
	}

	return nil
}
