package manifests

import (
	"fmt"
	"strings"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/settings"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManifestContext provides context for manifest execution
type ManifestContext struct {
	Stack              *v1beta1.Stack
	Module             v1beta1.Module
	Database           *v1beta1.Database
	Version            string
	ImageConfiguration *registries.ImageConfiguration
	AdditionalEnv      []corev1.EnvVar // Additional env vars from reconciler
}

// Apply executes the manifest against the cluster
func Apply(ctx core.Context, mctx ManifestContext, m *v1beta1.VersionManifest) error {
	// 1. Handle cleanup (for architecture transitions)
	if err := handleCleanup(ctx, mctx, m); err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	// 2. Deploy architecture
	switch m.Spec.Architecture.Type {
	case "stateless":
		return deployStateless(ctx, mctx, m)
	case "single-or-multi-writer":
		return deploySingleOrMultiWriter(ctx, mctx, m)
	default:
		return fmt.Errorf("unsupported architecture type: %s", m.Spec.Architecture.Type)
	}
}

func handleCleanup(ctx core.Context, mctx ManifestContext, m *v1beta1.VersionManifest) error {
	if len(m.Spec.Architecture.Cleanup.Deployments) == 0 {
		return nil
	}

	for _, name := range m.Spec.Architecture.Cleanup.Deployments {
		if err := core.DeleteIfExists[*appsv1.Deployment](
			ctx,
			core.GetNamespacedResourceName(mctx.Stack.Name, name),
		); err != nil {
			return err
		}
	}

	for _, name := range m.Spec.Architecture.Cleanup.Services {
		if err := core.DeleteIfExists[*corev1.Service](
			ctx,
			core.GetNamespacedResourceName(mctx.Stack.Name, name),
		); err != nil {
			return err
		}
	}

	return nil
}

func deployStateless(ctx core.Context, mctx ManifestContext, m *v1beta1.VersionManifest) error {
	for _, deploymentSpec := range m.Spec.Architecture.Deployments {
		if err := createDeployment(ctx, mctx, m, deploymentSpec); err != nil {
			return fmt.Errorf("creating deployment %s: %w", deploymentSpec.Name, err)
		}
	}
	return nil
}

func deploySingleOrMultiWriter(ctx core.Context, mctx ManifestContext, m *v1beta1.VersionManifest) error {
	// For now, just create the single deployment (simplified)
	// In a full implementation, this would check settings for strategy
	for _, deploymentSpec := range m.Spec.Architecture.Deployments {
		if err := createDeployment(ctx, mctx, m, deploymentSpec); err != nil {
			return fmt.Errorf("creating deployment %s: %w", deploymentSpec.Name, err)
		}
	}
	return nil
}

func createDeployment(ctx core.Context, mctx ManifestContext, m *v1beta1.VersionManifest, spec v1beta1.DeploymentSpec) error {
	// Build containers
	containers := make([]corev1.Container, 0, len(spec.Containers))
	for _, containerSpec := range spec.Containers {
		container, err := buildContainer(ctx, mctx, m, containerSpec)
		if err != nil {
			return err
		}
		containers = append(containers, *container)
	}

	// TODO: Get replicas from settings if needed
	// For now, applications.New handles replicas automatically
	_ = spec.Replicas // Silence unused warning

	// Get service account
	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, mctx.Stack.Name)
	if err != nil {
		return err
	}

	// Volumes (for v1 compatibility)
	var volumes []corev1.Volume
	if len(spec.Containers) > 0 && len(spec.Containers[0].VolumeMounts) > 0 {
		volumes = []corev1.Volume{{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}}
	}

	// Create deployment template
	tpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers:         containers,
					Volumes:            volumes,
					ServiceAccountName: serviceAccountName,
					ImagePullSecrets:   mctx.ImageConfiguration.PullSecrets,
				},
			},
		},
	}

	// Apply using existing applications helper
	app := applications.New(mctx.Module, tpl)
	if spec.Stateful {
		*app = app.WithStateful(true)
	}

	return app.Install(ctx)
}

func buildContainer(ctx core.Context, mctx ManifestContext, m *v1beta1.VersionManifest, spec v1beta1.ContainerSpec) (*corev1.Container, error) {
	container := &corev1.Container{
		Name:  spec.Name,
		Args:  spec.Args,
		Image: mctx.ImageConfiguration.GetFullImageName(),
	}

	// Ports
	for _, portSpec := range spec.Ports {
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          portSpec.Name,
			ContainerPort: portSpec.Port,
		})
	}

	// Health check
	if spec.HealthCheck != nil {
		switch spec.HealthCheck.Type {
		case "http":
			container.LivenessProbe = applications.DefaultLiveness("http")
		}
	}

	// Static environment variables
	for _, envVar := range spec.Environment {
		value := envVar.Value

		name := envVar.Name
		if m.Spec.EnvVarPrefix != "" {
			name = m.Spec.EnvVarPrefix + name
		}

		// Handle valueFrom
		if envVar.ValueFrom != nil && envVar.ValueFrom.SettingKey != "" {
			settingValue, err := settings.GetStringOrEmpty(
				ctx,
				mctx.Stack.Name,
				strings.Split(envVar.ValueFrom.SettingKey, ".")...,
			)
			if err != nil {
				return nil, err
			}
			value = settingValue
		}

		container.Env = append(container.Env, core.Env(name, value))
	}

	// Conditional environment variables
	for _, condEnv := range spec.ConditionalEnvironment {
		shouldApply, err := evaluateCondition(ctx, mctx, m, condEnv.When)
		if err != nil {
			return nil, err
		}

		if shouldApply {
			for _, envVar := range condEnv.Env {
				name := envVar.Name
				if m.Spec.EnvVarPrefix != "" {
					name = m.Spec.EnvVarPrefix + name
				}

				value := envVar.Value
				if envVar.ValueFrom != nil && envVar.ValueFrom.SettingKey != "" {
					settingValue, err := settings.GetStringOrEmpty(
						ctx,
						mctx.Stack.Name,
						strings.Split(envVar.ValueFrom.SettingKey, ".")...,
					)
					if err != nil {
						return nil, err
					}
					value = settingValue
				}

				container.Env = append(container.Env, core.Env(name, value))
			}
		}
	}

	// Volume mounts
	for _, vm := range spec.VolumeMounts {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      vm.Name,
			MountPath: vm.MountPath,
			ReadOnly:  vm.ReadOnly,
		})
	}

	// Add common environment variables (database, OTEL, etc.)
	commonEnv, err := buildCommonEnv(ctx, mctx, m)
	if err != nil {
		return nil, err
	}
	container.Env = append(container.Env, commonEnv...)

	return container, nil
}

func evaluateCondition(ctx core.Context, mctx ManifestContext, m *v1beta1.VersionManifest, condition string) (bool, error) {
	// Simple expression evaluator
	// Format: "settings.ledger.experimental-features == true"

	parts := strings.Fields(condition)
	if len(parts) != 3 {
		return false, fmt.Errorf("invalid condition format: %s", condition)
	}

	left := parts[0]
	operator := parts[1]
	right := parts[2]

	// Handle settings references
	if strings.HasPrefix(left, "settings.") {
		path := strings.TrimPrefix(left, "settings.")
		settingValue, err := settings.GetStringOrEmpty(ctx, mctx.Stack.Name, strings.Split(path, ".")...)
		if err != nil {
			return false, err
		}

		// If setting is empty, treat as false for boolean comparisons
		if settingValue == "" && right == "true" {
			return false, nil
		}

		left = settingValue
	}

	switch operator {
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	default:
		return false, fmt.Errorf("unsupported operator: %s", operator)
	}
}

func buildCommonEnv(ctx core.Context, mctx ManifestContext, m *v1beta1.VersionManifest) ([]corev1.EnvVar, error) {
	env := make([]corev1.EnvVar, 0)

	// OTEL env vars
	otlpEnv, err := settings.GetOTELEnvVarsWithPrefix(
		ctx,
		mctx.Stack.Name,
		core.LowerCamelCaseKind(ctx, mctx.Module),
		m.Spec.EnvVarPrefix,
		" ",
	)
	if err != nil {
		return nil, err
	}
	env = append(env, otlpEnv...)

	// Development env vars
	env = append(env, core.GetDevEnvVarsWithPrefix(mctx.Stack, mctx.Module, m.Spec.EnvVarPrefix)...)

	// Database env vars
	postgresEnv, err := databases.PostgresEnvVarsWithPrefix(
		ctx,
		mctx.Stack,
		mctx.Database,
		m.Spec.EnvVarPrefix,
	)
	if err != nil {
		return nil, err
	}
	env = append(env, postgresEnv...)

	// Additional env vars from reconciler (auth, gateway, broker, etc.)
	env = append(env, mctx.AdditionalEnv...)

	return env, nil
}
