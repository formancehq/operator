package wallets

import (
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/authclients"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/settings"
)

func createDeployment(ctx core.Context, stack *v1beta1.Stack, wallets *v1beta1.Wallets,
	authClient *v1beta1.AuthClient, version string) error {
	env := make([]v1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, core.LowerCamelCaseKind(ctx, wallets), " ")
	if err != nil {
		return err
	}
	env = append(env, otlpEnv...)

	gatewayEnv, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return err
	}
	env = append(env, gatewayEnv...)

	env = append(env, core.GetDevEnvVars(stack, wallets)...)
	if authClient != nil {
		env = append(env, authclients.GetEnvVars(authClient)...)
	}

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "wallets", wallets.Spec.Auth)
	if err != nil {
		return err
	}
	env = append(env, authEnvVars...)

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "wallets", version)
	if err != nil {
		return err
	}

	return applications.
		New(wallets, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "wallets",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ImagePullSecrets: imageConfiguration.PullSecrets,
						Containers: []v1.Container{{
							Name:           "wallets",
							Args:           []string{"serve"},
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
