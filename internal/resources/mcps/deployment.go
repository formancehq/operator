package mcps

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/applications"
	"github.com/formancehq/operator/v3/internal/resources/gateways"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func createDeployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	mcp *v1beta1.MCP,
	imageConfiguration *registries.ImageConfiguration,
) error {
	gateway := &v1beta1.Gateway{}
	ok, err := core.GetIfExists(ctx, stack.Name, gateway)
	if err != nil {
		return err
	}
	if !ok {
		return core.NewPendingError().WithMessage("gateway not found")
	}

	stackPublicURL := gateways.URL(gateway)

	env := []corev1.EnvVar{
		core.Env("BIND", ":8080"),
		core.Env("STACK_URL", "http://gateway:8080"),
		core.Env("STACK_PUBLIC_URL", stackPublicURL),
		core.Env("AUTH_ENABLED", "true"),
		core.Env("AUTH_ISSUER", fmt.Sprintf("%s/api/auth", stackPublicURL)),
		core.Env("AUTH_CHECK_SCOPES", "false"),
		core.Env("OTEL_SERVICE_NAME", serviceName),
	}

	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, serviceName, " ")
	if err != nil {
		return err
	}
	env = core.MergeEnvVars(env, otlpEnv)
	env = append(env, core.GetDevEnvVars(stack, mcp)...)

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	return applications.
		New(mcp, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: serviceName,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						ServiceAccountName: serviceAccountName,
						Containers: []corev1.Container{{
							Name:  serviceName,
							Args:  []string{"serve"},
							Env:   env,
							Image: imageConfiguration.GetFullImageName(),
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							Ports:          []corev1.ContainerPort{applications.StandardHTTPPort()},
							LivenessProbe:  applications.DefaultLiveness("http"),
							ReadinessProbe: applications.DefaultReadiness("http", applications.WithProbePath("/readyz")),
						}},
					},
				},
			},
		}).
		Install(ctx)
}
