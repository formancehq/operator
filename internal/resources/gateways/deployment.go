package gateways

import (
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/caddy"
	"github.com/formancehq/operator/internal/resources/registries"
	v1 "k8s.io/api/core/v1"
	"strings"
)

func createDeployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	gateway *v1beta1.Gateway,
	caddyfileConfigMap *v1.ConfigMap,
	broker *v1beta1.Broker,
	version string,
) error {

	env := GetEnvVars(gateway)
	env = append(env, core.GetDevEnvVars(stack, gateway)...)

	if broker != nil {
		brokerEnvVar, err := brokers.GetBrokerEnvVars(ctx, broker.Status.URI, stack.Name, "gateway")
		if err != nil {
			return err
		}

		env = append(env, brokerEnvVar...)

		parts := strings.SplitN(stack.Name, "-", 2)
		if len(parts) == 2 {
			env = append(env,
				core.Env("ORGANIZATION_ID", parts[0]),
				core.Env("STACK_ID", parts[1]),
			)
		}

		hasDependency, err := core.HasDependency(ctx, stack.Name, &v1beta1.Auth{})
		if err != nil {
			return err
		}
		if hasDependency {
			env = append(env,
				core.Env("AUTH_ENABLED", "true"),
				core.Env("AUTH_ISSUER", URL(gateway)+"/api/auth"),
			)
		}
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "gateway", version)
	if err != nil {
		return err
	}

	caddyTpl, err := caddy.DeploymentTemplate(ctx, stack, gateway, caddyfileConfigMap, imageConfiguration, env)
	if err != nil {
		return err
	}

	if broker != nil {
		var topicPrefix string
		switch broker.Status.Mode {
		case v1beta1.ModeOneStreamByService:
			topicPrefix = broker.Spec.Stack + "-"
		case v1beta1.ModeOneStreamByStack:
			topicPrefix = broker.Spec.Stack + "."
		}

		caddyTpl.Spec.Template.Spec.Containers[0].Env = append(
			caddyTpl.Spec.Template.Spec.Containers[0].Env,
			core.Env("TOPIC_NAME", topicPrefix+"gateway"),
		)
	}

	caddyTpl.Name = "gateway"
	return applications.
		New(gateway, caddyTpl).
		IsEE().
		Install(ctx)
}
