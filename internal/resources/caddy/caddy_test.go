package caddy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestComputeCaddyfileUsesTemplateFunctionsAndOpenTelemetryFlag(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(settings.New("otel", "opentelemetry.traces.dsn", "grpc://otel:4317", "stack0"))
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}

	out, err := ComputeCaddyfile(ctx, stack, `{{ if .EnableOpenTelemetry }}otel{{ end }} {{ join .Hosts "," }} {{ semver_compare "v2.0.0" "v1.0.0" }}`, map[string]any{
		"Hosts": []string{"a", "b"},
	})
	require.NoError(t, err)
	require.Equal(t, "otel a,b 1", strings.TrimSpace(out))
}

func TestDeploymentTemplate(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext()
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	gateway := &v1beta1.Gateway{}
	configMap := &corev1.ConfigMap{ObjectMeta: testutil.ObjectMeta("caddyfile"), Data: map[string]string{"Caddyfile": ":8080"}}
	image := &registries.ImageConfiguration{
		Registry: "docker.io",
		Image:    "caddy",
		Version:  "2.7.6",
		PullSecrets: []corev1.LocalObjectReference{
			{Name: "pull-secret"},
		},
	}

	deployment, err := DeploymentTemplate(ctx, stack, gateway, configMap, image, []corev1.EnvVar{{Name: "EXTRA", Value: "true"}})
	require.NoError(t, err)

	require.Equal(t, image.PullSecrets, deployment.Spec.Template.Spec.ImagePullSecrets)
	require.Equal(t, "docker.io/caddy:2.7.6", deployment.Spec.Template.Spec.Containers[0].Image)
	require.Equal(t, "true", testutil.EnvMap(deployment.Spec.Template.Spec.Containers[0].Env)["EXTRA"])
	require.Equal(t, "caddyfile", deployment.Spec.Template.Spec.Volumes[0].Name)
	require.Equal(t, "caddyfile", deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name)
	require.NotEmpty(t, deployment.Spec.Template.Annotations["caddyfile-hash"])
}
