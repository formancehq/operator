package applications

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestApplicationInstallAppliesDeploymentSettingsToFinalDeployment(t *testing.T) {
	t.Parallel()

	owner := applicationSettingsOwner()
	existingDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ledger",
			Namespace: "stack0",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RestartedAtAnnotationKey: "2026-05-11T10:00:00Z",
					},
				},
			},
		},
	}
	ctx := testutil.NewContext(append([]client.Object{
		existingDeployment,
		settingspkg.New("wildcard-annotations", "deployments.*.spec.template.annotations", "checksum=wildcard,team=platform", "stack0"),
		settingspkg.New("specific-annotations", "deployments.ledger.spec.template.annotations", "checksum=specific", "stack0"),
		settingspkg.New("replicas", "deployments.ledger.replicas", "3", "stack0"),
		settingspkg.New("container-env", "deployments.ledger.containers.api.env-vars", "EXISTING=override,FROM_SETTINGS=yes", "stack0"),
		settingspkg.New("init-env", "deployments.ledger.init-containers.init.env-vars", "INIT_FROM_SETTINGS=true", "stack0"),
		settingspkg.New("limits", "deployments.ledger.containers.api.resource-requirements.limits", "cpu=500m,memory=256Mi", "stack0"),
		settingspkg.New("requests", "deployments.ledger.containers.api.resource-requirements.requests", "cpu=250m,memory=128Mi", "stack0"),
		settingspkg.New("run-as", "deployments.ledger.containers.api.run-as", "user=1001,group=2001", "stack0"),
		settingspkg.New("topology", "deployments.ledger.topology-spread-constraints", "true", "stack0"),
		settingspkg.New("semconv", "deployments.ledger.semconv-metrics-names", "true", "stack0"),
		settingspkg.New("json-logging", "logging.json", "true", "stack0"),
		settingspkg.New("grace-period", "modules.ledger.grace-period", "30s", "stack0"),
		settingspkg.New("termination", "deployments.ledger.spec.template.spec.termination-grace-period-seconds", "45", "stack0"),
	}, owner)...)

	err := New(owner, applicationSettingsDeploymentTemplate()).Install(ctx)
	require.NoError(t, err)

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "ledger", Namespace: "stack0"}, deployment))

	require.Equal(t, int32(3), *deployment.Spec.Replicas)
	require.Equal(t, appsv1.RollingUpdateDeploymentStrategyType, deployment.Spec.Strategy.Type)
	require.Equal(t, int64(45), *deployment.Spec.Template.Spec.TerminationGracePeriodSeconds)
	require.Len(t, deployment.Spec.Template.Spec.TopologySpreadConstraints, 2)

	require.Equal(t, "2026-05-11T10:00:00Z", deployment.Spec.Template.Annotations[RestartedAtAnnotationKey])
	require.Equal(t, "specific", deployment.Spec.Template.Annotations["checksum"])
	require.NotContains(t, deployment.Spec.Template.Annotations, "team")

	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 1)
	initEnv := testutil.EnvMap(deployment.Spec.Template.Spec.InitContainers[0].Env)
	require.Equal(t, "base", initEnv["INIT_BASE"])
	require.Equal(t, "true", initEnv["INIT_FROM_SETTINGS"])
	require.Equal(t, "true", initEnv["JSON_FORMATTING_LOGGER"])
	require.Equal(t, "true", initEnv["SEMCONV_METRICS_NAME"])
	require.NotContains(t, initEnv, "GRACE_PERIOD")

	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	env := testutil.EnvMap(container.Env)
	require.Equal(t, "override", env["EXISTING"])
	require.Equal(t, "yes", env["FROM_SETTINGS"])
	require.Equal(t, "30s", env["GRACE_PERIOD"])
	require.Equal(t, "true", env["JSON_FORMATTING_LOGGER"])
	require.Equal(t, "true", env["SEMCONV_METRICS_NAME"])

	require.Equal(t, resource.MustParse("500m"), container.Resources.Limits[corev1.ResourceCPU])
	require.Equal(t, resource.MustParse("256Mi"), container.Resources.Limits[corev1.ResourceMemory])
	require.Equal(t, resource.MustParse("250m"), container.Resources.Requests[corev1.ResourceCPU])
	require.Equal(t, resource.MustParse("128Mi"), container.Resources.Requests[corev1.ResourceMemory])

	require.NotNil(t, deployment.Spec.Template.Spec.SecurityContext)
	require.True(t, *deployment.Spec.Template.Spec.SecurityContext.RunAsNonRoot)
	require.NotNil(t, container.SecurityContext)
	require.Equal(t, int64(1001), *container.SecurityContext.RunAsUser)
	require.Equal(t, int64(2001), *container.SecurityContext.RunAsGroup)
	require.True(t, *container.SecurityContext.RunAsNonRoot)
	require.False(t, *container.SecurityContext.Privileged)
	require.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
	require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
	require.Equal(t, []corev1.Capability{"all"}, container.SecurityContext.Capabilities.Drop)
}

func TestApplicationInstallAppliesPodDisruptionBudgetSettings(t *testing.T) {
	t.Parallel()

	owner := applicationSettingsOwner()
	ctx := testutil.NewContext(
		owner,
		settingspkg.New("pdb", "deployments.ledger.pod-disruption-budget", "minAvailable=1", "stack0"),
	)

	err := New(owner, applicationSettingsDeploymentTemplate()).Install(ctx)
	require.NoError(t, err)

	pdb := &policyv1.PodDisruptionBudget{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "ledger", Namespace: "stack0"}, pdb))
	require.Equal(t, "1", pdb.Spec.MinAvailable.String())
	require.Nil(t, pdb.Spec.MaxUnavailable)
	require.Equal(t, map[string]string{"app.kubernetes.io/name": "ledger"}, pdb.Spec.Selector.MatchLabels)

	condition := owner.Status.Conditions.Get("PodDisruptionBudgetConfigured")
	require.NotNil(t, condition)
	require.Equal(t, metav1.ConditionTrue, condition.Status)
	require.Equal(t, "Ledger", condition.Reason)
}

func TestApplicationInstallDeletesPDBForStatefulApplication(t *testing.T) {
	t.Parallel()

	owner := applicationSettingsOwner()
	existingPDB := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ledger",
			Namespace: "stack0",
		},
	}
	ctx := testutil.NewContext(
		owner,
		existingPDB,
		settingspkg.New("pdb", "deployments.ledger.pod-disruption-budget", "minAvailable=1", "stack0"),
	)

	err := New(owner, applicationSettingsDeploymentTemplate()).Stateful().Install(ctx)
	require.NoError(t, err)

	pdb := &policyv1.PodDisruptionBudget{}
	err = ctx.GetClient().Get(ctx, types.NamespacedName{Name: "ledger", Namespace: "stack0"}, pdb)
	require.Error(t, err)

	deployment := &appsv1.Deployment{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "ledger", Namespace: "stack0"}, deployment))
	require.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)
}

func TestApplicationInstallReturnsSettingsParsingErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		setting client.Object
	}{
		{
			name:    "invalid resource quantity",
			setting: settingspkg.New("invalid-quantity", "deployments.ledger.containers.api.resource-requirements.limits", "cpu=not-a-quantity", "stack0"),
		},
		{
			name:    "invalid run-as user",
			setting: settingspkg.New("invalid-run-as", "deployments.ledger.containers.api.run-as", "user=not-a-user", "stack0"),
		},
		{
			name:    "invalid termination grace period",
			setting: settingspkg.New("invalid-termination", "deployments.ledger.spec.template.spec.termination-grace-period-seconds", "not-an-int", "stack0"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			owner := applicationSettingsOwner()
			ctx := testutil.NewContext(owner, tc.setting)

			err := New(owner, applicationSettingsDeploymentTemplate()).Install(ctx)
			require.Error(t, err)
		})
	}
}

func applicationSettingsOwner() *v1beta1.Ledger {
	return &v1beta1.Ledger{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Ledger"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ledger",
			UID:        types.UID("ledger-uid"),
			Generation: 2,
		},
		Spec: v1beta1.LedgerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
}

func applicationSettingsDeploymentTemplate() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "ledger"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name:  "init",
						Image: "init:latest",
						Env: []corev1.EnvVar{{
							Name:  "INIT_BASE",
							Value: "base",
						}},
					}},
					Containers: []corev1.Container{{
						Name:  "api",
						Image: "ledger:latest",
						Env: []corev1.EnvVar{{
							Name:  "EXISTING",
							Value: "original",
						}},
					}},
				},
			},
		},
	}
}
