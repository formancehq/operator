package reconciliations

import (
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/applications"
	"github.com/formancehq/operator/v3/internal/resources/authclients"
	"github.com/formancehq/operator/v3/internal/resources/auths"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
	"github.com/formancehq/operator/v3/internal/resources/databases"
	"github.com/formancehq/operator/v3/internal/resources/gateways"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func deleteWorkerResources(ctx core.Context, stack string) error {
	if err := client.IgnoreNotFound(ctx.GetClient().Delete(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "reconciliation-worker", Namespace: stack},
	})); err != nil {
		return err
	}
	return client.IgnoreNotFound(ctx.GetClient().Delete(ctx, &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "reconciliation-worker", Namespace: stack},
	}))
}

func createDeployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	reconciliation *v1beta1.Reconciliation,
	database *v1beta1.Database,
	authClient *v1beta1.AuthClient,
	imageConfiguration *registries.ImageConfiguration,
) error {
	return createNamedDeployment(ctx, stack, reconciliation, database, authClient, imageConfiguration, nil, "reconciliation", "reconciliation", "reconciliation")
}

func createV3Deployments(
	ctx core.Context,
	stack *v1beta1.Stack,
	reconciliation *v1beta1.Reconciliation,
	database *v1beta1.Database,
	authClient *v1beta1.AuthClient,
	imageConfiguration *registries.ImageConfiguration,
	broker *v1beta1.Broker,
) error {
	// Roll the API first, then explicitly wait for every old replica to leave so
	// the worker never overlaps with a pre-v3 pod's embedded scheduler.
	if err := createNamedDeployment(ctx, stack, reconciliation, database, authClient, imageConfiguration, broker,
		"reconciliation", "reconciliation", "reconciliation", "serve"); err != nil {
		return err
	}
	apiDeployment := &appsv1.Deployment{}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: "reconciliation", Namespace: stack.Name}, apiDeployment); err != nil {
		return err
	}
	if !deploymentRolloutComplete(apiDeployment) {
		return core.NewPendingError().WithMessage("waiting for reconciliation API rollout before starting worker")
	}
	return createNamedDeployment(ctx, stack, reconciliation, database, authClient, imageConfiguration, broker,
		"reconciliation-worker", "reconciliation-worker", "reconciliation-worker", "worker")
}

func deploymentRolloutComplete(deployment *appsv1.Deployment) bool {
	if deployment.Spec.Replicas == nil || deployment.Status.ObservedGeneration != deployment.Generation {
		return false
	}
	desired := *deployment.Spec.Replicas
	return deployment.Status.UpdatedReplicas >= desired &&
		deployment.Status.Replicas == deployment.Status.UpdatedReplicas &&
		deployment.Status.AvailableReplicas >= desired
}

func createNamedDeployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	reconciliation *v1beta1.Reconciliation,
	database *v1beta1.Database,
	authClient *v1beta1.AuthClient,
	imageConfiguration *registries.ImageConfiguration,
	broker *v1beta1.Broker,
	deploymentName, containerName, otelServiceName string,
	args ...string,
) error {
	env := make([]v1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, otelServiceName, " ")
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
	env = append(env, core.GetDevEnvVars(stack, reconciliation)...)
	env = append(env, postgresEnvVar...)
	env = append(env, core.Env("POSTGRES_DATABASE_NAME", "$(POSTGRES_DATABASE)"))
	env = append(env, authclients.GetEnvVars(authClient)...)

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "reconciliation", reconciliation.Spec.Auth)
	if err != nil {
		return err
	}
	env = append(env, authEnvVars...)
	if broker != nil {
		brokerEnv, err := brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, deploymentName)
		if err != nil {
			return err
		}
		env = append(env, brokerEnv...)
		env = append(env, brokers.GetPublisherEnvVars(stack, broker, "reconciliation")...)
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	return applications.
		New(reconciliation, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: deploymentName,
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						ServiceAccountName: serviceAccountName,
						Containers: []v1.Container{{
							Name:           containerName,
							Args:           args,
							Env:            env,
							Image:          imageConfiguration.GetFullImageName(),
							Ports:          []v1.ContainerPort{applications.StandardHTTPPort()},
							LivenessProbe:  applications.DefaultLiveness("http"),
							ReadinessProbe: applications.DefaultReadiness("http"),
						}},
					},
				},
			},
		}).
		IsEE().
		Install(ctx)
}
