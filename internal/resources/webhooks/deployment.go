package webhooks

import (
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/settings"
)

func deploymentEnvVars(ctx core.Context, stack *v1beta1.Stack, webhooks *v1beta1.Webhooks, database *v1beta1.Database) ([]v1.EnvVar, error) {

	brokerURI, err := settings.RequireURL(ctx, stack.Name, "broker", "dsn")
	if err != nil {
		return nil, err
	}
	if brokerURI == nil {
		return nil, errors.New("missing broker configuration")
	}

	env := make([]v1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, core.LowerCamelCaseKind(ctx, webhooks), " ")
	if err != nil {
		return nil, err
	}
	env = append(env, otlpEnv...)

	gatewayEnv, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return nil, err
	}
	env = append(env, gatewayEnv...)

	env = append(env, core.GetDevEnvVars(stack, webhooks)...)

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "webhooks", webhooks.Spec.Auth)
	if err != nil {
		return nil, err
	}

	postgresEnvVar, err := databases.GetPostgresEnvVars(ctx, stack, database)
	if err != nil {
		return nil, err
	}

	brokerEnvVar, err := brokers.GetBrokerEnvVars(ctx, brokerURI, stack.Name, "webhooks")
	if err != nil {
		return nil, err
	}

	env = append(env, authEnvVars...)
	env = append(env, postgresEnvVar...)
	env = append(env, brokerEnvVar...)
	env = append(env, core.Env("STORAGE_POSTGRES_CONN_STRING", "$(POSTGRES_URI)"))

	return env, nil
}

func createAPIDeployment(ctx core.Context, stack *v1beta1.Stack, webhooks *v1beta1.Webhooks, database *v1beta1.Database, consumer *v1beta1.BrokerConsumer, version string, withWorker bool) error {

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "webhooks", version)
	if err != nil {
		return err
	}

	env, err := deploymentEnvVars(ctx, stack, webhooks, database)
	if err != nil {
		return err
	}

	args := []string{"serve"}

	// notes(gfyrag): upgrade command introduced in version v2.0.0-rc.5
	if core.IsGreaterOrEqual(version, "v2.0.0-alpha") && core.IsLower(version, "v2.0.0-rc.5") {
		args = append(args, "--auto-migrate")
	}
	if withWorker {
		env = append(env, core.Env("WORKER", "true"))

		topics, err := brokers.GetTopicsEnvVars(ctx, stack, "KAFKA_TOPICS", consumer.Spec.Services...)
		if err != nil {
			return err
		}
		env = append(env, topics...)
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	return applications.
		New(webhooks, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "webhooks",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []v1.Container{{
							Name:          "api",
							Env:           env,
							Image:         imageConfiguration.GetFullImageName(),
							Args:          args,
							Ports:         []v1.ContainerPort{applications.StandardHTTPPort()},
							LivenessProbe: applications.DefaultLiveness("http"),
						}},
					},
				},
			},
		}).
		IsEE().
		Install(ctx)
}

func createSingleDeployment(ctx core.Context, stack *v1beta1.Stack, webhooks *v1beta1.Webhooks, database *v1beta1.Database, consumer *v1beta1.BrokerConsumer, version string) error {
	return createAPIDeployment(ctx, stack, webhooks, database, consumer, version, true)
}
