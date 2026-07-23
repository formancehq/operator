package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pulumi/pulumi-docker-build/sdk/go/dockerbuild"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"gopkg.in/yaml.v3"
)

func getBuildVersion(gitDir string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = gitDir
	output, err := cmd.Output()

	timestamp := time.Now().Format("20060102-150405")

	if err != nil {
		return timestamp
	}

	commit := strings.TrimSpace(string(output))

	cmd = exec.Command("git", "status", "--porcelain")
	cmd.Dir = gitDir
	statusOutput, _ := cmd.Output()

	if len(statusOutput) > 0 {
		return fmt.Sprintf("%s-dirty-%s", commit, timestamp)
	}

	return fmt.Sprintf("%s-%s", commit, timestamp)
}

func getConfigBool(cfg *config.Config, key string, fallback bool) bool {
	value := cfg.GetBool(key)
	if value {
		return true
	}
	if cfg.Get(key) == "false" {
		return false
	}
	return fallback
}

func newK8sProvider(ctx *pulumi.Context, cfg *config.Config) (pulumi.ProviderResource, error) {
	kubeContext := cfg.Require("k8s-context")

	k8sProvider, err := kubernetes.NewProvider(ctx, "k8s", &kubernetes.ProviderArgs{
		Context: pulumi.StringPtr(kubeContext),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s provider: %w", err)
	}

	return k8sProvider, nil
}

type dockerConfig struct {
	Registry     string
	PullRegistry string
	BuilderName  string
	ImageTag     string
	Platforms    []string
	RegistryAuth dockerbuild.RegistryArray
}

var allPlatforms = []string{"linux-amd64", "linux-arm64"}

func newDockerConfig(ctx *pulumi.Context, cfg *config.Config) *dockerConfig {
	registry := cfg.Get("registry")
	if registry == "" {
		registry = "ghcr.io"
	}
	pullRegistry := cfg.Get("pull-registry")
	if pullRegistry == "" {
		pullRegistry = registry
	}
	builderName := cfg.Get("docker-builder-name")

	buildVersion := getBuildVersion("../..")
	imageTag := cfg.Get("imageTag")
	if imageTag == "" {
		imageTag = buildVersion
	}

	arch := cfg.Get("arch")
	platforms := append([]string(nil), allPlatforms...)
	if arch != "" {
		platforms = platforms[:0]
		for _, p := range allPlatforms {
			if strings.HasSuffix(p, arch) {
				platforms = append(platforms, p)
			}
		}
		if len(platforms) == 0 {
			platforms = []string{"linux-" + arch}
		}
	}

	return &dockerConfig{
		Registry:     registry,
		PullRegistry: pullRegistry,
		BuilderName:  builderName,
		ImageTag:     imageTag,
		Platforms:    platforms,
		RegistryAuth: dockerbuild.RegistryArray{
			dockerbuild.RegistryArgs{
				Address:  pulumi.String(registry),
				Username: config.GetSecret(ctx, "registry-username"),
				Password: config.GetSecret(ctx, "registry-password"),
			},
		},
	}
}

type multiArchImage struct {
	Index  *dockerbuild.Index
	Images []*dockerbuild.Image
	Ref    pulumi.StringOutput
	Digest pulumi.StringOutput
}

func (m *multiArchImage) Resource() pulumi.Resource {
	return m.Index
}

func (dc *dockerConfig) buildImage(
	ctx *pulumi.Context,
	name string,
	contextPath string,
	dockerfilePath string,
) (*multiArchImage, error) {
	var sources pulumi.StringArray
	var images []*dockerbuild.Image

	for _, platform := range dc.Platforms {
		img, err := dockerbuild.NewImage(ctx, fmt.Sprintf("%s-%s", name, platform), &dockerbuild.ImageArgs{
			Context: dockerbuild.BuildContextArgs{
				Location: pulumi.String(contextPath),
			},
			Builder: dockerbuild.BuilderConfigArgs{
				Name: pulumi.String(dc.BuilderName),
			},
			CacheFrom: dockerbuild.CacheFromArray{
				dockerbuild.CacheFromArgs{
					Registry: dockerbuild.CacheFromRegistryArgs{
						Ref: pulumi.Sprintf("%s/%s:buildcache-%s", dc.Registry, name, platform),
					},
				},
			},
			CacheTo: dockerbuild.CacheToArray{
				dockerbuild.CacheToArgs{
					Registry: dockerbuild.CacheToRegistryArgs{
						Ref:  pulumi.Sprintf("%s/%s:buildcache-%s", dc.Registry, name, platform),
						Mode: dockerbuild.CacheModeMax,
					},
				},
			},
			Dockerfile: dockerbuild.DockerfileArgs{
				Location: pulumi.String(dockerfilePath),
			},
			Platforms: dockerbuild.PlatformArray{
				dockerbuild.Platform(strings.ReplaceAll(platform, "-", "/")),
			},
			Push:       pulumi.Bool(true),
			Registries: dc.RegistryAuth,
			Tags: pulumi.StringArray{
				pulumi.Sprintf("%s/%s:%s-%s", dc.Registry, name, dc.ImageTag, platform),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to build %s for %s: %w", name, platform, err)
		}
		sources = append(sources, img.Ref)
		images = append(images, img)
	}

	idx, err := dockerbuild.NewIndex(ctx, name, &dockerbuild.IndexArgs{
		Sources: sources,
		Tag:     pulumi.Sprintf("%s/%s:%s", dc.Registry, name, dc.ImageTag),
		Push:    pulumi.Bool(true),
		Registry: dockerbuild.RegistryArgs{
			Address:  pulumi.String(dc.Registry),
			Username: dc.RegistryAuth[0].(dockerbuild.RegistryArgs).Username,
			Password: dc.RegistryAuth[0].(dockerbuild.RegistryArgs).Password,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create index for %s: %w", name, err)
	}

	digest := idx.Ref.ApplyT(func(ref string) string {
		if i := strings.Index(ref, "@"); i >= 0 {
			return ref[i+1:]
		}
		return ref
	}).(pulumi.StringOutput)

	return &multiArchImage{
		Index:  idx,
		Images: images,
		Ref:    idx.Ref,
		Digest: digest,
	}, nil
}

func getConfigMap(cfg *config.Config, key string) pulumi.Map {
	var obj map[string]any
	if err := cfg.GetObject(key, &obj); err != nil || obj == nil {
		return pulumi.Map{}
	}
	return pulumi.ToMap(obj)
}

func getConfigArray(cfg *config.Config, key string) pulumi.Array {
	var arr []map[string]any
	if err := cfg.GetObject(key, &arr); err != nil || arr == nil {
		return pulumi.Array{}
	}
	result := make(pulumi.Array, len(arr))
	for i, v := range arr {
		result[i] = pulumi.ToMap(v)
	}
	return result
}

func getImagePullSecrets(cfg *config.Config) pulumi.Array {
	var secrets []map[string]any
	if err := cfg.GetObject("image-pull-secrets", &secrets); err != nil || len(secrets) == 0 {
		return pulumi.Array{}
	}
	var result pulumi.Array
	for _, s := range secrets {
		if name, ok := s["name"].(string); ok && name != "" {
			result = append(result, pulumi.Map{
				"name": pulumi.String(name),
			})
		}
	}
	return result
}

func getConfigObject(cfg *config.Config, key string, basePath string) (map[string]any, error) {
	var configObj map[string]any
	if err := cfg.GetObject(key, &configObj); err != nil {
		return nil, fmt.Errorf("failed to get config object %s: %w", key, err)
	}

	if filePath, ok := configObj["file"].(string); ok {
		fullPath := filepath.Join(basePath, filePath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read values file %s: %w", fullPath, err)
		}

		var result map[string]any
		if err := yaml.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse YAML file %s: %w", fullPath, err)
		}

		return result, nil
	}

	return configObj, nil
}
