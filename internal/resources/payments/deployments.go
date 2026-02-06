package payments

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/caddy"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/resourcereferences"
	"github.com/formancehq/operator/internal/resources/services"
	"github.com/formancehq/operator/internal/resources/settings"
)

func getEncryptionKey(ctx core.Context, payments *v1beta1.Payments) (string, error) {
	encryptionKey := payments.Spec.EncryptionKey
	if encryptionKey == "" {
		return settings.GetStringOrEmpty(ctx, payments.Spec.Stack, "payments", "encryption-key")
	}
	return "", nil
}

func temporalEnvVars(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments) (hash map[string]string, env []corev1.EnvVar, err error) {
	hash = map[string]string{}
	var (
		ref         *v1beta1.ResourceReference
		temporalURI *v1beta1.URI
	)

	temporalURI, err = settings.RequireURL(ctx, stack.Name, "temporal", "dsn")
	if err != nil {
		return
	}

	if err = validateTemporalURI(temporalURI); err != nil {
		return
	}

	if secret := temporalURI.Query().Get("secret"); secret != "" {
		ref, err = resourcereferences.Create(ctx, payments, "payments-temporal", secret, &corev1.Secret{})
		hash[ref.Name] = ref.Status.Hash
	} else {
		err = resourcereferences.Delete(ctx, payments, "payments-temporal")
	}
	if err != nil {
		return
	}

	if secret := temporalURI.Query().Get("encryptionKeySecret"); secret != "" {
		ref, err = resourcereferences.Create(ctx, payments, "payments-temporal-encryption-key", secret, &corev1.Secret{})
		hash[ref.Name] = ref.Status.Hash
	} else {
		err = resourcereferences.Delete(ctx, payments, "payments-temporal-encryption-key")
	}

	if err != nil {
		return
	}

	env = make([]corev1.EnvVar, 0)
	env = append(env,
		core.Env("TEMPORAL_ADDRESS", temporalURI.Host),
		core.Env("TEMPORAL_NAMESPACE", temporalURI.Path[1:]),
	)

	if secret := temporalURI.Query().Get("secret"); secret == "" {
		var value string
		value, err = settings.GetStringOrEmpty(ctx, stack.Name, "temporal", "tls", "crt")
		if err != nil {
			return
		}
		env = append(env,
			core.Env("TEMPORAL_SSL_CLIENT_CERT", value),
		)

		value, err = settings.GetStringOrEmpty(ctx, stack.Name, "temporal", "tls", "key")
		if err != nil {
			return
		}

		env = append(env,
			core.Env("TEMPORAL_SSL_CLIENT_KEY", value),
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

	var value int
	value, err = settings.GetIntOrDefault(ctx, stack.Name, 4, "payments", "worker", "temporal-max-concurrent-workflow-task-pollers")
	if err != nil {
		return
	}
	env = append(env,
		core.Env("TEMPORAL_MAX_CONCURRENT_WORKFLOW_TASK_POLLERS", fmt.Sprintf("%d", value)),
	)

	value, err = settings.GetIntOrDefault(ctx, stack.Name, 4, "payments", "worker", "temporal-max-concurrent-activity-task-pollers")
	if err != nil {
		return
	}
	env = append(env,
		core.Env("TEMPORAL_MAX_CONCURRENT_ACTIVITY_TASK_POLLERS", fmt.Sprintf("%d", value)),
	)

	value, err = settings.GetIntOrDefault(ctx, stack.Name, 10, "payments", "worker", "temporal-max-slots-per-poller")
	if err != nil {
		return
	}
	env = append(env,
		core.Env("TEMPORAL_MAX_SLOTS_PER_POLLER", fmt.Sprintf("%d", value)),
	)

	value, err = settings.GetIntOrDefault(ctx, stack.Name, 50, "payments", "worker", "temporal-max-local-activity-slots")
	if err != nil {
		return
	}

	env = append(env,
		core.Env("TEMPORAL_MAX_LOCAL_ACTIVITY_SLOTS", fmt.Sprintf("%d", value)),
	)

	return
}

func commonEnvVars(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments, database *v1beta1.Database) ([]corev1.EnvVar, error) {
	env := make([]corev1.EnvVar, 0)
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

func deleteDeployment(ctx core.Context, stack *v1beta1.Stack, name string) error {
	deploymentName := core.GetNamespacedResourceName(stack.Name, name)
	deployment := &appsv1.Deployment{}

	if err := ctx.GetClient().Get(ctx, deploymentName, deployment); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Deployment doesn't exist, successfully deleted
			return nil
		}
		return err
	}

	// Check if deployment is already being deleted
	if !deployment.GetDeletionTimestamp().IsZero() {
		// Deployment is still being deleted, wait for it to complete
		return core.NewPendingError().WithMessage("waiting for deployment %s to be deleted", name)
	}

	// Deployment exists and is not being deleted, delete it now
	if err := ctx.GetClient().Delete(ctx, deployment); err != nil {
		return err
	}

	// Return pending error to wait for deletion to complete
	return core.NewPendingError().WithMessage("waiting for deployment %s to be deleted", name)
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
) error {
	// The deployment order matters here: Temporal workflows created on old code will not be successfully executed on
	// old workers, so we need to make sure worker deployment happen before API deployment.
	err := createV3Deployment(ctx, stack, payments, database, imageConfiguration, DeploymentTypeWorker)
	if err != nil {
		return err
	}
	err = createV3Deployment(ctx, stack, payments, database, imageConfiguration, DeploymentTypeApi)
	if err != nil {
		return err
	}

	return nil
}

const DeploymentTypeWorker = "worker"
const DeploymentTypeApi = "api"

func createV3Deployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	payments *v1beta1.Payments,
	database *v1beta1.Database,
	imageConfiguration *registries.ImageConfiguration,
	deploymentType string,
) error {
	var (
		containerName string
		metaName      string
		arg           string
	)

	switch deploymentType {
	case DeploymentTypeWorker:
		containerName = "payments-worker"
		metaName = "payments-worker"
		arg = "worker"
	case DeploymentTypeApi:
		containerName = "payments-api"
		metaName = "payments"
		arg = "server"
	default:
		return fmt.Errorf("invalid deployment type: %s", deploymentType)
	}

	hashMap, env, err := v3EnvVars(ctx, stack, payments, database)
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	appOpts := applications.WithProbePath("/_healthcheck")

	err = applications.
		New(payments, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: metaName,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: hashMap,
					},
					Spec: corev1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []corev1.Container{{
							Name:           containerName,
							Args:           []string{arg},
							Env:            env,
							Image:          imageConfiguration.GetFullImageName(),
							LivenessProbe:  applications.DefaultLiveness("http", appOpts),
							ReadinessProbe: applications.DefaultReadiness("http", appOpts),
							Ports:          []corev1.ContainerPort{applications.StandardHTTPPort()},
						}},
						// Ensure empty
						InitContainers: []corev1.Container{},
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

func v3EnvVars(
	ctx core.Context,
	stack *v1beta1.Stack,
	payments *v1beta1.Payments,
	database *v1beta1.Database,
) (
	hash map[string]string,
	envVars []corev1.EnvVar,
	err error,
) {

	envVars, err = commonEnvVars(ctx, stack, payments, database)
	if err != nil {
		return
	}

	var (
		additionalEnv []corev1.EnvVar
	)

	additionalEnv, err = auths.ProtectedEnvVars(ctx, stack, "payments", payments.Spec.Auth)
	if err != nil {
		return
	}
	envVars = append(envVars, additionalEnv...)

	var (
		broker *v1beta1.Broker
		topic  *v1beta1.BrokerTopic
	)
	if topic, err = brokertopics.Find(ctx, stack, "payments"); err != nil {
		return
	} else if topic != nil && topic.Status.Ready {
		broker = &v1beta1.Broker{}
		if err = ctx.GetClient().Get(ctx, types.NamespacedName{
			Name: stack.Name,
		}, broker); err != nil {
			return
		}
	}

	if broker != nil {
		if !broker.Status.Ready {
			err = core.NewPendingError().WithMessage("broker not ready")
			return
		}
		additionalEnv, err = brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, "payments")
		if err != nil {
			return
		}

		envVars = append(envVars, additionalEnv...)
		envVars = append(envVars, brokers.GetPublisherEnvVars(stack, broker, "payments")...)
	}

	hash, additionalEnv, err = temporalEnvVars(ctx, stack, payments)
	if err != nil {
		return
	}
	envVars = append(envVars, additionalEnv...)

	return
}

func createV2ReadDeployment(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration) error {

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
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []corev1.Container{{
							Name:          "api",
							Args:          []string{"api", "serve"},
							Env:           env,
							Image:         imageConfiguration.GetFullImageName(),
							LivenessProbe: applications.DefaultLiveness("http", applications.WithProbePath("/_health")),
							Ports:         []corev1.ContainerPort{applications.StandardHTTPPort()},
						}},
						// Ensure empty
						InitContainers: []corev1.Container{},
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

func createV2ConnectorsDeployment(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments, database *v1beta1.Database, imageConfiguration *registries.ImageConfiguration) error {

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
		env = append(env, brokers.GetPublisherEnvVars(stack, broker, "payments")...)
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
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []corev1.Container{{
							Name:  "connectors",
							Args:  []string{"connectors", "serve"},
							Env:   env,
							Image: imageConfiguration.GetFullImageName(),
							Ports: []corev1.ContainerPort{applications.StandardHTTPPort()},
							LivenessProbe: applications.DefaultLiveness("http",
								applications.WithProbePath("/_health")),
						}},
						// Ensure empty
						InitContainers: []corev1.Container{},
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
	}, core.WithController[*corev1.ConfigMap](ctx.GetScheme(), p))
	if err != nil {
		return err
	}

	env := make([]corev1.EnvVar, 0)

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
	deploymentTemplate.Spec.Template.Spec.InitContainers = make([]corev1.Container, 0)

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
