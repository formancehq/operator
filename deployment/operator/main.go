package main

import (
	"fmt"
	"path/filepath"
	"strings"

	k8syaml "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/yaml"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")

		k8s, err := newK8sSetup(ctx, cfg)
		if err != nil {
			return err
		}
		namespace := k8s.Namespace
		k8sProvider := k8s.Provider

		dc := newDockerConfig(ctx, cfg)

		// Build operator image
		operatorImage, err := dc.buildImage(ctx, "formancehq/operator", "../..", "../../Dockerfile")
		if err != nil {
			return fmt.Errorf("failed to build operator image: %w", err)
		}

		// Apply CRDs
		crdFiles, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "*.yaml"))
		if err != nil {
			return fmt.Errorf("failed to glob CRD files: %w", err)
		}

		var crds []pulumi.Resource
		for _, crdFile := range crdFiles {
			name := strings.TrimSuffix(filepath.Base(crdFile), filepath.Ext(crdFile))
			crd, crdErr := k8syaml.NewConfigFile(ctx, name+"-crd", &k8syaml.ConfigFileArgs{
				File: crdFile,
			}, pulumi.Provider(k8sProvider))
			if crdErr != nil {
				return fmt.Errorf("failed to apply CRD %s: %w", name, crdErr)
			}
			crds = append(crds, crd)
		}

		// Operator configuration
		region := cfg.Get("operator-region")
		if region == "" {
			region = "eu-west-1"
		}
		env := cfg.Get("operator-env")
		if env == "" {
			env = "staging"
		}

		// Licence configuration
		licenceValues := pulumi.Map{}
		licenceToken := cfg.Get("licence-token")
		if licenceToken != "" {
			licenceIssuer := cfg.Get("licence-issuer")
			if licenceIssuer == "" {
				licenceIssuer = "https://license.formance.cloud/keys"
			}
			licenceValues = pulumi.Map{
				"createSecret": pulumi.Bool(true),
				"token":        pulumi.String(licenceToken),
				"issuer":       pulumi.String(licenceIssuer),
			}
		} else {
			licenceValues = pulumi.Map{
				"createSecret": pulumi.Bool(false),
			}
		}

		// Deploy operator via Helm
		operatorChartPath := filepath.Join("..", "..", "helm", "operator")

		operatorRelease, err := helm.NewRelease(ctx, "formance-operator", &helm.ReleaseArgs{
			Name:      pulumi.String("formance-operator"),
			Chart:     pulumi.String(operatorChartPath),
			Namespace: namespace.Metadata.Name(),
			Values: pulumi.Map{
				"operator-crds": pulumi.Map{
					"create": pulumi.Bool(false),
				},
				"image": pulumi.Map{
					"repository": pulumi.Sprintf("%s/formancehq/operator", dc.PullRegistry),
					"tag":        pulumi.Sprintf("latest@%s", operatorImage.Digest),
				},
				"imagePullSecrets": getImagePullSecrets(cfg),
				"global": pulumi.Map{
					"licence": licenceValues,
				},
				"operator": pulumi.Map{
					"region":               pulumi.String(region),
					"env":                  pulumi.String(env),
					"dev":                  pulumi.Bool(getConfigBool(cfg, "operator-dev", false)),
					"enableLeaderElection": pulumi.Bool(true),
				},
				"nodeSelector": getConfigMap(cfg, "node-selector"),
				"tolerations":  getConfigArray(cfg, "tolerations"),
			},
			ForceUpdate: pulumi.Bool(true),
		},
			pulumi.DependsOn(append([]pulumi.Resource{namespace, operatorImage.Resource()}, crds...)),
			pulumi.Provider(k8sProvider),
		)
		if err != nil {
			return fmt.Errorf("failed to deploy operator: %w", err)
		}

		// Exports
		ctx.Export("namespace", namespace.Metadata.Name())
		ctx.Export("operatorImage", pulumi.Sprintf("%s/formancehq/operator:latest@%s", dc.PullRegistry, operatorImage.Digest))
		ctx.Export("operatorRelease", operatorRelease.Name)

		return nil
	})
}
