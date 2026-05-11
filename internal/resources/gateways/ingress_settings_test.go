package gateways

import (
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestCreateIngressAppliesSettingsToFinalIngress(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	gateway := gatewaySettingsFixture()
	ctx := testutil.NewContext(
		stack,
		gateway,
		settingspkg.New("hosts", "gateway.ingress.hosts", "{stack}.settings.example.com, extra.example.com, spec.example.com", "stack0"),
		settingspkg.New("annotations-wildcard", "gateway.ingress.annotations", "from=settings,override=settings", "*"),
		settingspkg.New("annotations-specific", "gateway.ingress.annotations", "override=specific", "stack0"),
		settingspkg.New("labels", "gateway.ingress.labels", "team=edge", "stack0"),
		settingspkg.New("tls", "gateway.ingress.tls.enabled", "true", "stack0"),
		settingspkg.New("class", "gateway.ingress.class", "nginx", "stack0"),
	)

	require.NoError(t, createIngress(ctx, stack, gateway))

	ingress := &networkingv1.Ingress{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "gateway", Namespace: "stack0"}, ingress))
	require.Equal(t, map[string]string{
		"override": "gateway-spec",
	}, ingress.Annotations)
	require.Equal(t, "edge", ingress.Labels["team"])
	require.Equal(t, "gateway", ingress.Labels["app.kubernetes.io/component"])
	require.Equal(t, "stack0", ingress.Labels["app.kubernetes.io/name"])
	require.Equal(t, "nginx", *ingress.Spec.IngressClassName)

	require.Len(t, ingress.Spec.Rules, 4)
	require.Equal(t, []string{
		"spec.example.com",
		"alt.example.com",
		"stack0.settings.example.com",
		"extra.example.com",
	}, ingressHosts(ingress))
	require.Len(t, ingress.Spec.TLS, 1)
	require.Equal(t, "gateway-tls", ingress.Spec.TLS[0].SecretName)
	require.Equal(t, ingressHosts(ingress), ingress.Spec.TLS[0].Hosts)
	require.Len(t, ingress.OwnerReferences, 1)
	require.Equal(t, "Gateway", ingress.OwnerReferences[0].Kind)
}

func TestCreateIngressPrefersGatewaySpecTLSAndIngressClass(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	className := "from-spec"
	gateway := gatewaySettingsFixture()
	gateway.Spec.Ingress.IngressClassName = &className
	gateway.Spec.Ingress.TLS = &v1beta1.GatewayIngressTLS{SecretName: "spec-tls"}
	ctx := testutil.NewContext(
		stack,
		gateway,
		settingspkg.New("tls", "gateway.ingress.tls.enabled", "true", "stack0"),
		settingspkg.New("class", "gateway.ingress.class", "nginx", "stack0"),
	)

	require.NoError(t, createIngress(ctx, stack, gateway))

	ingress := &networkingv1.Ingress{}
	require.NoError(t, ctx.GetClient().Get(ctx, types.NamespacedName{Name: "gateway", Namespace: "stack0"}, ingress))
	require.Equal(t, "from-spec", *ingress.Spec.IngressClassName)
	require.Len(t, ingress.Spec.TLS, 1)
	require.Equal(t, "spec-tls", ingress.Spec.TLS[0].SecretName)
}

func TestCreateIngressDeletesIngressWhenGatewayIngressIsNil(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
	gateway := gatewaySettingsFixture()
	gateway.Spec.Ingress = nil
	existing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "stack0"}}
	ctx := testutil.NewContext(stack, gateway, existing)

	require.NoError(t, createIngress(ctx, stack, gateway))

	ingress := &networkingv1.Ingress{}
	err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: "gateway", Namespace: "stack0"}, ingress)
	require.Error(t, err)
}

func TestCreateIngressReturnsInvalidSettingsErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		setting *v1beta1.Settings
	}{
		{
			name:    "invalid annotations",
			setting: settingspkg.New("invalid-annotations", "gateway.ingress.annotations", `foo="unterminated`, "stack0"),
		},
		{
			name:    "invalid labels",
			setting: settingspkg.New("invalid-labels", "gateway.ingress.labels", `foo="unterminated`, "stack0"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}
			gateway := gatewaySettingsFixture()
			ctx := testutil.NewContext(stack, gateway, tc.setting)

			require.Error(t, createIngress(ctx, stack, gateway))
		})
	}
}

func ingressHosts(ingress *networkingv1.Ingress) []string {
	hosts := make([]string, 0, len(ingress.Spec.Rules))
	for _, rule := range ingress.Spec.Rules {
		hosts = append(hosts, rule.Host)
	}
	return hosts
}

func gatewaySettingsFixture() *v1beta1.Gateway {
	return &v1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Gateway"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "gateway",
			UID:  types.UID("gateway-uid"),
		},
		Spec: v1beta1.GatewaySpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
			Ingress: &v1beta1.GatewayIngress{
				Host:  "spec.example.com",
				Hosts: []string{"alt.example.com"},
				Annotations: map[string]string{
					"override": "gateway-spec",
				},
			},
		},
	}
}
