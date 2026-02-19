package transactions

import (
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/authclients"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/settings"
)

func createAuthClient(ctx Context, stack *v1beta1.Stack, t *v1beta1.Transactions) (*v1beta1.AuthClient, error) {
	hasAuth, err := HasDependency(ctx, stack.Name, &v1beta1.Auth{})
	if err != nil {
		return nil, err
	}
	if !hasAuth {
		return nil, nil
	}

	return authclients.Create(ctx, stack, t, "transactions",
		func(spec *v1beta1.AuthClientSpec) {
			spec.Scopes = []string{
				"ledger:read",
				"ledger:write",
				"payments:read",
				"payments:write",
			}
		})
}

func createDeployment(
	ctx Context,
	stack *v1beta1.Stack,
	t *v1beta1.Transactions,
	database *v1beta1.Database,
	client *v1beta1.AuthClient,
	consumer *v1beta1.BrokerConsumer,
	imageConfiguration *registries.ImageConfiguration,
) error {

	env := make([]corev1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, LowerCamelCaseKind(ctx, t), " ")
	if err != nil {
		return err
	}
	env = append(env, otlpEnv...)

	gatewayEnv, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return err
	}

	postgresEnvVar, err := databases.GetPostgresEnvVars(ctx, stack, database)
	if err != nil {
		return err
	}

	env = append(env, gatewayEnv...)
	env = append(env, GetDevEnvVars(stack, t)...)
	env = append(env, postgresEnvVar...)

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "transactions", t.Spec.Auth)
	if err != nil {
		return err
	}
	env = append(env, authEnvVars...)

	if client != nil {
		env = append(env, authclients.GetEnvVars(client)...)
	}

	topics, err := brokers.GetTopicsEnvVars(ctx, stack, "TOPICS", consumer.Spec.Services...)
	if err != nil {
		return err
	}
	env = append(env, topics...)

	broker := &v1beta1.Broker{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Name: stack.Name,
	}, broker); err != nil {
		return err
	}

	brokerEnvVars, err := brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, "transactions")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	env = append(env, brokerEnvVars...)
	env = append(env, brokers.GetPublisherEnvVars(stack, broker, "transactions")...)

	workerEnabled, err := settings.GetBoolOrDefault(ctx, stack.Name, false, "transactions", "worker-enabled")
	if err != nil {
		return err
	}
	env = append(env, EnvFromBool("WORKER_ENABLED", workerEnabled))

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "transactions",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ImagePullSecrets:   imageConfiguration.PullSecrets,
					ServiceAccountName: serviceAccountName,
					Containers: []corev1.Container{{
						Name:           "transactions",
						Args:           []string{"serve"},
						Env:            env,
						Image:          imageConfiguration.GetFullImageName(),
						Ports:          []corev1.ContainerPort{applications.StandardHTTPPort()},
						LivenessProbe:  applications.DefaultLiveness("http"),
						ReadinessProbe: applications.DefaultReadiness("http"),
					}},
				},
			},
		},
	}

	return applications.
		New(t, tpl).
		Install(ctx)
}
