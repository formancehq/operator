package reconciliations

import (
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/authclients"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/settings"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createDeployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	reconciliation *v1beta1.Reconciliation,
	database *v1beta1.Database,
	authClient *v1beta1.AuthClient,
	imageConfiguration *registries.ImageConfiguration,
) error {
	env := make([]v1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, core.LowerCamelCaseKind(ctx, reconciliation), " ")
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

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	return applications.
		New(reconciliation, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "reconciliation",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						ServiceAccountName: serviceAccountName,
						Containers: []v1.Container{{
							Name:          "reconciliation",
							Env:           env,
							Image:         imageConfiguration.GetFullImageName(),
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
