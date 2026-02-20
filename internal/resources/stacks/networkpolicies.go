package stacks

import (
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
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

	return nil
}

func deleteNetworkPolicies(ctx Context, stack *v1beta1.Stack) error {
	for _, name := range []string{"default-deny-ingress", "allow-gateway-ingress", "allow-from-gateway"} {
		if err := DeleteIfExists[*networkingv1.NetworkPolicy](ctx, types.NamespacedName{
			Namespace: stack.Name,
			Name:      name,
		}); err != nil {
			return err
		}
	}
	return nil
}
