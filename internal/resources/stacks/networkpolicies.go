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
	// 1. default-deny-ingress: block all ingress traffic to all pods
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{
			Namespace: stack.Name,
			Name:      "default-deny-ingress",
		},
		func(np *networkingv1.NetworkPolicy) error {
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	// 2. allow-gateway-ingress: allow all ingress traffic to gateway pods
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{
			Namespace: stack.Name,
			Name:      "allow-gateway-ingress",
		},
		func(np *networkingv1.NetworkPolicy) error {
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/name": "gateway",
					},
				},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress:     []networkingv1.NetworkPolicyIngressRule{{}},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	// 3. allow-from-gateway: allow ingress only from gateway pods to all pods
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{
			Namespace: stack.Name,
			Name:      "allow-from-gateway",
		},
		func(np *networkingv1.NetworkPolicy) error {
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From: []networkingv1.NetworkPolicyPeer{
							{
								PodSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app.kubernetes.io/name": "gateway",
									},
								},
							},
						},
					},
				},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	// 4. allow-ledger-v3-cluster: allow traffic between direct Ledger v3
	// replicas. Ports are intentionally unrestricted within the cluster so
	// LedgerConfiguration service port overrides cannot break Raft or gRPC.
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{
			Namespace: stack.Name,
			Name:      "allow-ledger-v3-cluster",
		},
		func(np *networkingv1.NetworkPolicy) error {
			selector := directLedgerV3Selector(stack.Name)
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: selector,
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From: []networkingv1.NetworkPolicyPeer{
							{PodSelector: &selector},
						},
					},
				},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	// 5. allow-ledger-v3-preview-cluster: use the historical preview label so
	// existing clusters do not require immutable selector changes.
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{
			Namespace: stack.Name,
			Name:      "allow-ledger-v3-preview-cluster",
		},
		func(np *networkingv1.NetworkPolicy) error {
			selector := previewLedgerV3Selector()
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: selector,
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					From: []networkingv1.NetworkPolicyPeer{{PodSelector: &selector}},
				}},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	// 6. allow-ledger-v2-from-v3: let the preview cluster read only the legacy
	// Ledger pods in the same Stack namespace. A PodSelector without a
	// NamespaceSelector never matches pods from another namespace.
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{
			Namespace: stack.Name,
			Name:      "allow-ledger-v2-from-v3",
		},
		func(np *networkingv1.NetworkPolicy) error {
			directSelector := directLedgerV3Selector(stack.Name)
			previewSelector := previewLedgerV3Selector()
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
					"app.kubernetes.io/name": "ledger",
				}},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From: []networkingv1.NetworkPolicyPeer{
							{PodSelector: &directSelector},
							{PodSelector: &previewSelector},
						},
						Ports: networkPolicyTCPPorts(8080),
					},
				},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	// 7. allow-ledger-v3-from-connectivity: let the delegated connectivity
	// workload reach the stack's Ledger v3 pods (it dials their gRPC endpoint).
	// Connectivity pods are not Ledger v3 pods, so the default-deny policy would
	// otherwise drop those connections. Ports are intentionally unrestricted for
	// this tightly scoped same-namespace source/target pair — mirroring
	// allow-ledger-v3-cluster — so a LedgerConfiguration spec.cluster.service.grpcPort
	// override cannot silently break connectivity or leave the policy stale.
	if _, _, err := CreateOrUpdate[*networkingv1.NetworkPolicy](ctx,
		types.NamespacedName{
			Namespace: stack.Name,
			Name:      "allow-ledger-v3-from-connectivity",
		},
		func(np *networkingv1.NetworkPolicy) error {
			ledgerSelector := directLedgerV3Selector(stack.Name)
			connectivity := connectivitySelector()
			np.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: ledgerSelector,
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From: []networkingv1.NetworkPolicyPeer{{PodSelector: &connectivity}},
					},
				},
			}
			return nil
		},
		WithController[*networkingv1.NetworkPolicy](ctx.GetScheme(), stack),
	); err != nil {
		return err
	}

	return nil
}

func deleteNetworkPolicies(ctx Context, stack *v1beta1.Stack) error {
	for _, name := range []string{
		"default-deny-ingress",
		"allow-gateway-ingress",
		"allow-from-gateway",
		"allow-ledger-v3-cluster",
		"allow-ledger-v3-preview-cluster",
		"allow-ledger-v2-from-v3",
		"allow-ledger-v3-from-connectivity",
	} {
		if err := DeleteIfExists[*networkingv1.NetworkPolicy](ctx, types.NamespacedName{
			Namespace: stack.Name,
			Name:      name,
		}); err != nil {
			return err
		}
	}
	return nil
}

// connectivitySelector matches the delegated connectivity workload pods that
// dial the stack's Ledger v3 gRPC endpoint.
//
// ASSUMPTION: the connectivity pods are provisioned by the connectivity
// operator (connectivity.formance.com), which lives in a separate repository,
// so their pod labels are not defined in this repo and cannot be confirmed
// here. We match the operator-wide convention app.kubernetes.io/name=<component>
// with the "connectivity" component name (the connectivity core image
// repository is likewise "connectivity"). We intentionally match only the name
// label (a subset match) to avoid over-constraining on labels we cannot verify.
// If the connectivity operator labels its pods differently, this selector must
// be reconciled against that repository.
func connectivitySelector() metav1.LabelSelector {
	return metav1.LabelSelector{MatchLabels: map[string]string{
		"app.kubernetes.io/name": "connectivity",
	}}
}

func directLedgerV3Selector(stackName string) metav1.LabelSelector {
	return metav1.LabelSelector{MatchLabels: map[string]string{
		"app.kubernetes.io/instance": stackName,
		"app.kubernetes.io/name":     "ledger",
	}}
}

func previewLedgerV3Selector() metav1.LabelSelector {
	return metav1.LabelSelector{MatchLabels: map[string]string{
		"formance.com/ledger-v3-preview": "true",
	}}
}

func networkPolicyTCPPorts(ports ...int) []networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	ret := make([]networkingv1.NetworkPolicyPort, 0, len(ports))
	for _, port := range ports {
		value := intstr.FromInt(port)
		ret = append(ret, networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value})
	}
	return ret
}
