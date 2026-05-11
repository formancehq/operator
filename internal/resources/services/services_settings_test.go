package services

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestCreateAppliesServiceSettings(t *testing.T) {
	t.Parallel()

	owner := serviceSettingsOwner()
	ctx := testutil.NewContext(
		owner,
		settingspkg.New("wildcard-annotations", "services.*.annotations", "team=platform,checksum=wildcard", "stack0"),
		settingspkg.New("specific-annotations", "services.ledger.annotations", "checksum=specific", "stack0"),
		settingspkg.New("traffic", "services.ledger.traffic-distribution", "PreferClose", "stack0"),
	)

	service, err := Create(ctx, owner, "ledger", WithDefault("ledger"))
	require.NoError(t, err)

	stored := &corev1.Service{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "ledger", Namespace: "stack0"}, stored))
	require.Equal(t, service.Name, stored.Name)

	require.Equal(t, map[string]string{"app.kubernetes.io/service-name": "ledger"}, stored.Labels)
	require.Equal(t, map[string]string{"app.kubernetes.io/name": "ledger"}, stored.Spec.Selector)
	require.Len(t, stored.Spec.Ports, 1)
	require.Equal(t, int32(8080), stored.Spec.Ports[0].Port)
	require.Equal(t, "http", stored.Spec.Ports[0].TargetPort.String())

	require.Equal(t, "specific", stored.Annotations["checksum"])
	require.NotContains(t, stored.Annotations, "team")
	require.NotNil(t, stored.Spec.TrafficDistribution)
	require.Equal(t, "PreferClose", *stored.Spec.TrafficDistribution)
	require.Len(t, stored.OwnerReferences, 1)
	require.Equal(t, "Ledger", stored.OwnerReferences[0].Kind)
}

func TestCreateUsesWildcardServiceSettingsWhenNoSpecificSettingExists(t *testing.T) {
	t.Parallel()

	owner := serviceSettingsOwner()
	ctx := testutil.NewContext(
		owner,
		settingspkg.New("wildcard-annotations", "services.*.annotations", "team=platform", "stack0"),
	)

	service, err := Create(ctx, owner, "ledger", WithDefault("ledger"))
	require.NoError(t, err)
	require.Equal(t, "platform", service.Annotations["team"])
}

func TestCreateReturnsInvalidServiceAnnotationSettingError(t *testing.T) {
	t.Parallel()

	owner := serviceSettingsOwner()
	ctx := testutil.NewContext(
		owner,
		settingspkg.New("invalid-annotations", "services.ledger.annotations", `team="unterminated`, "stack0"),
	)

	_, err := Create(ctx, owner, "ledger", WithDefault("ledger"))
	require.Error(t, err)
}

func serviceSettingsOwner() *v1beta1.Ledger {
	return &v1beta1.Ledger{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Ledger"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "ledger",
			UID:  types.UID("ledger-uid"),
		},
		Spec: v1beta1.LedgerSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
}
