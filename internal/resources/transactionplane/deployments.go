package transactionplane

import (
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/applications"
	"github.com/formancehq/operator/v3/internal/resources/authclients"
	"github.com/formancehq/operator/v3/internal/resources/auths"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
	"github.com/formancehq/operator/v3/internal/resources/databases"
	"github.com/formancehq/operator/v3/internal/resources/gateways"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func createAuthClient(ctx Context, stack *v1beta1.Stack, t *v1beta1.TransactionPlane) (*v1beta1.AuthClient, error) {
	hasAuth, err := HasDependency(ctx, stack.Name, &v1beta1.Auth{})
	if err != nil {
		return nil, err
	}
	if !hasAuth {
		return nil, nil
	}

	return authclients.Create(ctx, stack, t, "transactionplane",
		func(spec *v1beta1.AuthClientSpec) {
			spec.Scopes = []string{
				"ledger:read",
				"ledger:write",
				"payments:read",
				"payments:write",
			}
		})
}

func commonEnvVars(
	ctx Context,
	stack *v1beta1.Stack,
	t *v1beta1.TransactionPlane,
	database *v1beta1.Database,
	client *v1beta1.AuthClient,
	consumer *v1beta1.BrokerConsumer,
) ([]corev1.EnvVar, error) {
	env := make([]corev1.EnvVar, 0)

	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, LowerCamelCaseKind(ctx, t), " ")
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
	env = append(env, GetDevEnvVars(stack, t)...)
	env = append(env, postgresEnvVar...)

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "transactionplane", t.Spec.Auth)
	if err != nil {
		return nil, err
	}
	env = append(env, authEnvVars...)

	if client != nil {
		env = append(env, authclients.GetEnvVars(client)...)
	}

	topics, err := brokers.GetTopicsEnvVars(ctx, stack, "TOPICS", consumer.Spec.Services...)
	if err != nil {
		return nil, err
	}
	env = append(env, topics...)

	broker := &v1beta1.Broker{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Name: stack.Name,
	}, broker); err != nil {
		return nil, err
	}

	brokerEnvVars, err := brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, "transactionplane")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	env = append(env, brokerEnvVars...)
	env = append(env, brokers.GetPublisherEnvVars(stack, broker, "transactionplane")...)

	return env, nil
}

func createDeployments(
	ctx Context,
	stack *v1beta1.Stack,
	t *v1beta1.TransactionPlane,
	database *v1beta1.Database,
	client *v1beta1.AuthClient,
	consumer *v1beta1.BrokerConsumer,
	imageConfiguration *registries.ImageConfiguration,
) error {
	env, err := commonEnvVars(ctx, stack, t, database, client, consumer)
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	workerEnabled, err := settings.GetBoolOrDefault(ctx, stack.Name, false, "transactionplane", "worker-enabled")
	if err != nil {
		return err
	}

	if workerEnabled {
		if err := deleteDeployment(ctx, stack, "transactionplane-worker"); err != nil {
			return err
		}
		return createSingleDeployment(ctx, t, imageConfiguration, serviceAccountName, env)
	}

	return createSeparateDeployments(ctx, t, imageConfiguration, serviceAccountName, env)
}

func deleteDeployment(ctx Context, stack *v1beta1.Stack, name string) error {
	deployment := &appsv1.Deployment{}
	if err := ctx.GetClient().Get(ctx, GetNamespacedResourceName(stack.Name, name), deployment); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		return err
	}

	if !deployment.GetDeletionTimestamp().IsZero() {
		return NewPendingError().WithMessage("waiting for deployment %s to be deleted", name)
	}

	LogDeletion(ctx, deployment, "transactionplane.deleteDeployment")
	if err := ctx.GetClient().Delete(ctx, deployment); err != nil {
		return err
	}

	return NewPendingError().WithMessage("waiting for deployment %s to be deleted", name)
}

func createSingleDeployment(
	ctx Context,
	t *v1beta1.TransactionPlane,
	imageConfiguration *registries.ImageConfiguration,
	serviceAccountName string,
	env []corev1.EnvVar,
) error {
	env = append(env, Env("WORKER_ENABLED", "true"))

	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "transactionplane",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ImagePullSecrets:   imageConfiguration.PullSecrets,
					ServiceAccountName: serviceAccountName,
					Containers: []corev1.Container{{
						Name:           "transactionplane",
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

func createSeparateDeployments(
	ctx Context,
	t *v1beta1.TransactionPlane,
	imageConfiguration *registries.ImageConfiguration,
	serviceAccountName string,
	env []corev1.EnvVar,
) error {
	// Worker deployment first (same pattern as payments: deploy worker before API)
	workerTpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "transactionplane-worker",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ImagePullSecrets:   imageConfiguration.PullSecrets,
					ServiceAccountName: serviceAccountName,
					Containers: []corev1.Container{{
						Name:           "transactionplane-worker",
						Args:           []string{"worker"},
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

	if err := applications.New(t, workerTpl).Install(ctx); err != nil {
		return err
	}

	// API deployment
	apiTpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "transactionplane",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ImagePullSecrets:   imageConfiguration.PullSecrets,
					ServiceAccountName: serviceAccountName,
					Containers: []corev1.Container{{
						Name:           "transactionplane",
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
		New(t, apiTpl).
		Install(ctx)
}
