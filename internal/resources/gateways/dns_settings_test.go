package gateways

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsv1alpha1 "sigs.k8s.io/external-dns/apis/v1alpha1"
	"sigs.k8s.io/external-dns/endpoint"

	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestReconcileDNSEndpointsAppliesSettingsToFinalDNSEndpoints(t *testing.T) {
	t.Parallel()

	gateway := gatewaySettingsFixture()
	ctx := testutil.NewContext(
		gateway,
		settingspkg.New("private-enabled", "gateway.dns.private.enabled", "true", "stack0"),
		settingspkg.New("private-names", "gateway.dns.private.dns-names", "{stack}.internal.example.com, api.internal.example.com", "stack0"),
		settingspkg.New("private-targets", "gateway.dns.private.targets", "10.0.0.1, 10.0.0.2", "stack0"),
		settingspkg.New("private-provider", "gateway.dns.private.provider-specific", "aws-zone-type=private,aws-weight=100", "stack0"),
		settingspkg.New("private-annotations", "gateway.dns.private.annotations", "owner=platform", "stack0"),
		settingspkg.New("private-record", "gateway.dns.private.record-type", "A", "stack0"),
		settingspkg.New("public-enabled", "gateway.dns.public.enabled", "true", "stack0"),
		settingspkg.New("public-names", "gateway.dns.public.dns-names", "{stack}.public.example.com", "stack0"),
		settingspkg.New("public-targets", "gateway.dns.public.targets", "gateway.example.net", "stack0"),
	)

	require.NoError(t, reconcileDNSEndpoints(ctx, gateway))

	privateEndpoint := &externaldnsv1alpha1.DNSEndpoint{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "gateway-private", Namespace: "stack0"}, privateEndpoint))
	require.Equal(t, "platform", privateEndpoint.Annotations["owner"])
	require.Len(t, privateEndpoint.OwnerReferences, 1)
	require.Equal(t, "Gateway", privateEndpoint.OwnerReferences[0].Kind)
	require.Len(t, privateEndpoint.Spec.Endpoints, 2)
	require.Equal(t, "stack0.internal.example.com", privateEndpoint.Spec.Endpoints[0].DNSName)
	require.Equal(t, "A", privateEndpoint.Spec.Endpoints[0].RecordType)
	require.Equal(t, endpoint.Targets{"10.0.0.1", "10.0.0.2"}, privateEndpoint.Spec.Endpoints[0].Targets)
	require.ElementsMatch(t, endpoint.ProviderSpecific{
		{Name: "aws-zone-type", Value: "private"},
		{Name: "aws-weight", Value: "100"},
	}, privateEndpoint.Spec.Endpoints[0].ProviderSpecific)
	require.Equal(t, "api.internal.example.com", privateEndpoint.Spec.Endpoints[1].DNSName)

	publicEndpoint := &externaldnsv1alpha1.DNSEndpoint{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "gateway-public", Namespace: "stack0"}, publicEndpoint))
	require.Len(t, publicEndpoint.Spec.Endpoints, 1)
	require.Equal(t, "stack0.public.example.com", publicEndpoint.Spec.Endpoints[0].DNSName)
	require.Equal(t, "CNAME", publicEndpoint.Spec.Endpoints[0].RecordType)
	require.Equal(t, endpoint.Targets{"gateway.example.net"}, publicEndpoint.Spec.Endpoints[0].Targets)
}

func TestReconcileDNSEndpointsDeletesDisabledEndpoints(t *testing.T) {
	t.Parallel()

	gateway := gatewaySettingsFixture()
	existingPrivate := &externaldnsv1alpha1.DNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "gateway-private", Namespace: "stack0"},
	}
	existingPublic := &externaldnsv1alpha1.DNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{Name: "gateway-public", Namespace: "stack0"},
	}
	ctx := testutil.NewContext(gateway, existingPrivate, existingPublic)

	require.NoError(t, reconcileDNSEndpoints(ctx, gateway))

	err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: "gateway-private", Namespace: "stack0"}, &externaldnsv1alpha1.DNSEndpoint{})
	require.Error(t, err)
	err = ctx.GetClient().Get(ctx, types.NamespacedName{Name: "gateway-public", Namespace: "stack0"}, &externaldnsv1alpha1.DNSEndpoint{})
	require.Error(t, err)
}

func TestGetDNSConfigRequiresNamesAndTargetsWhenEnabled(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		settings []client.Object
	}{
		{
			name: "missing dns names",
			settings: []client.Object{
				settingspkg.New("enabled", "gateway.dns.private.enabled", "true", "stack0"),
				settingspkg.New("targets", "gateway.dns.private.targets", "10.0.0.1", "stack0"),
			},
		},
		{
			name: "missing targets",
			settings: []client.Object{
				settingspkg.New("enabled", "gateway.dns.private.enabled", "true", "stack0"),
				settingspkg.New("names", "gateway.dns.private.dns-names", "stack0.example.com", "stack0"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := testutil.NewContext(tc.settings...)

			_, err := getDNSConfig(ctx, "stack0", "private")
			require.Error(t, err)
		})
	}
}
