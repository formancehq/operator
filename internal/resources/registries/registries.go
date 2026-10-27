package registries

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

// ghcr.io/<organization>/<repository>:<version>
// public.ecr.aws/<organization>/jeffail/benthos:<version>
// docker.io/<organization|user>/<image>:<version>
// version: "v2.0.0-rc.35-scratch@sha256:4a29620448a90f3ae50d2e375c993b86ef141ead4b6ac1edd1674e9ff6b933f8"
// docker.io/<organization|user>/<image>:v2.0.0-rc.35-scratch@sha256:4a29620448a90f3ae50d2e375c993b86ef141ead4b6ac1edd1674e9ff6b933f8
type ImageConfiguration struct {
	Registry    string
	Image       string
	Version     string
	PullSecrets []v1.LocalObjectReference
}

func (cfg *ImageConfiguration) GetFullImageName() string {
	ret := ""
	if cfg.Registry != "" {
		ret += cfg.Registry + "/"
	}
	ret += cfg.Image + ":" + cfg.Version

	return ret
}

func (o *ImageConfiguration) String() string {
	return fmt.Sprintf("%s/%s:%s", o.Registry, o.Image, o.Version)
}

func GetImageConfiguration(
	ctx core.Context,
	stackName string,
	image string,
) (*ImageConfiguration, error) {
	repository, version, found := strings.Cut(image, ":")
	if !found {
		return nil, fmt.Errorf("invalid image format: %s", image)
	}

	organizationImage := strings.SplitN(repository, "/", 2)
	var (
		registry             string
		imageWithoutRegistry string
	)
	if len(organizationImage) > 1 {
		registry = organizationImage[0]
		imageWithoutRegistry = organizationImage[1]
	} else {
		registry = "docker.io"
		imageWithoutRegistry = repository
	}

	ret := &ImageConfiguration{
		Registry: registry,
		Image:    imageWithoutRegistry,
		Version:  version,
	}

	imageOverride, err := settings.GetStringOrEmpty(ctx, stackName, "registries", registry, "images", imageWithoutRegistry, "rewrite")
	if err != nil {
		return nil, err
	}
	if imageOverride != "" {
		ret.Image = imageOverride
	}

	registryEndpoint, err := settings.GetStringOrEmpty(ctx, stackName, "registries", registry, "endpoint")
	if err != nil {
		return nil, err
	}
	if registryEndpoint != "" {
		ret.Registry, ret.PullSecrets, err = parseEndpoint(registryEndpoint)
		if err != nil {
			return nil, err
		}
	}

	return ret, nil
}

func parseEndpoint(endpoint string) (string, []v1.LocalObjectReference, error) {
	parts := strings.SplitN(endpoint, "?", 2)
	var secrets []v1.LocalObjectReference
	if len(parts) > 1 {
		for _, pair := range strings.Split(parts[1], ",") {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return "", nil, fmt.Errorf("invalid registry endpoint parameter")
			}
			if key != "pullSecret" {
				return "", nil, fmt.Errorf("unknown registry endpoint parameter: %s", key)
			}
			if value == "" {
				return "", nil, fmt.Errorf("registry pull secret name must not be empty")
			}
			secrets = append(secrets, v1.LocalObjectReference{Name: value})
		}
	}
	return parts[0], secrets, nil
}
