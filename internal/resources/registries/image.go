package registries

import (
	"fmt"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
)

// Format Accepted:
// ENDPOINT/ORGANIZATION/REPOSITORY:VERSION
// ghcr.io/<organization>/<repository>:<version>
// public.ecr.aws/<organization>/jeffail/benthos
// docker.io/<organization|user>/<image>:<version>

func NormalizeVersion(version string) string {
	if version == "" {
		version = "latest"
	}
	return version
}

func GetFormanceImage(ctx core.Context, stack *v1beta1.Stack, name, version string) (*ImageConfiguration, error) {
	return GetImageConfiguration(
		ctx,
		stack.Name,
		fmt.Sprintf("ghcr.io/formancehq/%s:%s", name, NormalizeVersion(version)),
	)
}

func GetBenthosImage(ctx core.Context, stack *v1beta1.Stack, version string) (*ImageConfiguration, error) {
	return GetImageConfiguration(
		ctx,
		stack.Name,
		fmt.Sprintf("public.ecr.aws/formance-internal/jeffail/benthos:%s", NormalizeVersion(version)),
	)
}

func GetNatsBoxImage(ctx core.Context, stack *v1beta1.Stack, version string) (*ImageConfiguration, error) {
	return GetImageConfiguration(
		ctx,
		stack.Name,
		fmt.Sprintf("docker.io/natsio/nats-box:%s", NormalizeVersion(version)),
	)
}

func GetCaddyImage(ctx core.Context, stack *v1beta1.Stack) (*ImageConfiguration, error) {
	defaultCaddyImage := "caddy:2.7.6-alpine"
	selectedCaddyImage, err := settings.GetStringOrDefault(ctx, stack.Name, defaultCaddyImage, "caddy", "image")
	if err != nil {
		return nil, err
	}

	return GetImageConfiguration(ctx, stack.Name, selectedCaddyImage)
}
