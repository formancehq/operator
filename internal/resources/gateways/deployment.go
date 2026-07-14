package gateways

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/applications"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
	"github.com/formancehq/operator/v3/internal/resources/caddy"
	"github.com/formancehq/operator/v3/internal/resources/registries"
)

func createDeployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	gateway *v1beta1.Gateway,
	caddyfileConfigMap *v1.ConfigMap,
	httpAPIs []*v1beta1.GatewayHTTPAPI,
	grpcAPIs []*v1beta1.GatewayGRPCAPI,
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
	if err := configureBackendTLSVolumes(ctx, stack.Name, caddyTpl, httpAPIs, grpcAPIs); err != nil {
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

func configureBackendTLSVolumes(
	ctx core.Context,
	namespace string,
	deployment *appsv1.Deployment,
	httpAPIs []*v1beta1.GatewayHTTPAPI,
	grpcAPIs []*v1beta1.GatewayGRPCAPI,
) error {
	secretNames := map[string]struct{}{}
	for _, httpAPI := range httpAPIs {
		for _, rule := range httpAPI.Spec.Rules {
			if rule.BackendRef != nil && rule.BackendRef.TLS != nil {
				secretNames[rule.BackendRef.TLS.SecretName] = struct{}{}
			}
		}
	}
	for _, grpcAPI := range grpcAPIs {
		if grpcAPI.Spec.BackendRef != nil && grpcAPI.Spec.BackendRef.TLS != nil {
			secretNames[grpcAPI.Spec.BackendRef.TLS.SecretName] = struct{}{}
		}
	}

	sortedSecretNames := make([]string, 0, len(secretNames))
	for secretName := range secretNames {
		sortedSecretNames = append(sortedSecretNames, secretName)
	}
	sort.Strings(sortedSecretNames)
	secretsDigest := sha256.New()
	for _, secretName := range sortedSecretNames {
		secret := &v1.Secret{}
		if err := ctx.GetClient().Get(ctx, core.GetNamespacedResourceName(namespace, secretName), secret); err != nil {
			return fmt.Errorf("getting backend TLS Secret %s/%s: %w", namespace, secretName, err)
		}
		secretData, err := json.Marshal(secret.Data)
		if err != nil {
			return fmt.Errorf("hashing backend TLS Secret %s/%s: %w", namespace, secretName, err)
		}
		_, _ = secretsDigest.Write([]byte(secretName))
		_, _ = secretsDigest.Write(secretData)

		digest := sha256.Sum256([]byte(secretName))
		volumeName := fmt.Sprintf("backend-tls-%x", digest[:6])
		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, v1.Volume{
			Name: volumeName,
			VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{
				SecretName: secretName,
			}},
		})
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
			core.NewVolumeMount(volumeName, "/etc/gateway/tls/"+secretName, true),
		)
	}
	if len(sortedSecretNames) > 0 {
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations["formance.com/backend-tls-secrets-hash"] = fmt.Sprintf("%x", secretsDigest.Sum(nil))
	}
	return nil
}
