package registries

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"strings"

	"github.com/formancehq/operator/internal/resources/settings"

	"github.com/formancehq/operator/internal/core"
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
		parts := strings.SplitN(registryEndpoint, "?", 2)
		ret.Registry = parts[0]
		if len(parts) > 1 {
			parametersPairs := strings.Split(parts[1], ",")
			for _, pair := range parametersPairs {
				parts := strings.SplitN(pair, "=", 2)
				if len(parts) != 2 {
					return nil, fmt.Errorf("invalid registry endpoint parameter: %s", pair)
				}
				switch parts[0] {
				case "pullSecret":
					ret.PullSecrets = append(ret.PullSecrets, v1.LocalObjectReference{
						Name: parts[1],
					})
				default:
					return nil, fmt.Errorf("unknown registry endpoint parameter: %s", parts[0])
				}
			}
		}
	}

	return ret, nil
}
