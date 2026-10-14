package stacks

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

// Retire the previous per-module ingress rules when reconciling existing Stacks.
var legacyNetworkPolicyNames = []string{
	"default-deny-ingress",
	"allow-from-gateway",
	"allow-ledger-v3-cluster",
	"allow-ledger-v3-preview-cluster",
	"allow-ledger-v2-from-v3",
	"allow-ledger-v3-from-connectivity",
}

func reconcileNetworkPolicies(ctx Context, stack *v1beta1.Stack) error {
	enabled, err := settings.GetBoolOrFalse(ctx, stack.Name, "networkpolicies", "enabled")
	if err != nil {
		return err
	}
	if enabled {
		return createNetworkPolicies(ctx, stack)
	}
	return deleteNetworkPolicies(ctx, stack)
}

func createNetworkPolicies(ctx Context, stack *v1beta1.Stack) error {
	// Selecting every pod isolates ingress by default. A PodSelector without a
	// NamespaceSelector allows only sources in the same Stack namespace.
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{Namespace: stack.Name, Name: "allow-same-namespace"},
		func(np *networkingv1.NetworkPolicy) error {
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
				}},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	// Cross-namespace and external callers must enter through the Gateway's
	// HTTP/gRPC listener. Its admin/metrics listener is not exposed by this rule.
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{Namespace: stack.Name, Name: "allow-gateway-ingress"},
		func(np *networkingv1.NetworkPolicy) error {
			protocol := corev1.ProtocolTCP
			port := intstr.FromInt32(8080)
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
					"app.kubernetes.io/name": "gateway",
				}},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}},
				}},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	// Install both replacements before deleting the old rules to preserve local
	// access throughout an upgrade. Other controllers' policies are left intact.
	return deleteNamedNetworkPolicies(ctx, stack, legacyNetworkPolicyNames)
}

func deleteNetworkPolicies(ctx Context, stack *v1beta1.Stack) error {
	names := append([]string{"allow-same-namespace", "allow-gateway-ingress"}, legacyNetworkPolicyNames...)
	return deleteNamedNetworkPolicies(ctx, stack, names)
}

func deleteNamedNetworkPolicies(ctx Context, stack *v1beta1.Stack, names []string) error {
	for _, name := range names {
		if err := DeleteIfExists[*networkingv1.NetworkPolicy](ctx, types.NamespacedName{
			Namespace: stack.Name,
			Name:      name,
		}); err != nil {
			return err
		}
	}
	return nil
}
