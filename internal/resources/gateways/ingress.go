package gateways

import (
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
)

func withAnnotations(ctx core.Context, stack *v1beta1.Stack, gateway *v1beta1.Gateway) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
		annotations, err := settings.GetMap(ctx, stack.Name, "gateway", "ingress", "annotations")
		if err != nil {
			return err
		}
		if annotations == nil {
			annotations = map[string]string{}
		}

		if gateway.Spec.Ingress.Annotations != nil {
			for k, v := range gateway.Spec.Ingress.Annotations {
				annotations[k] = v
			}
		}

		t.SetAnnotations(annotations)

		return nil
	}
}

func withLabels(ctx core.Context, stack *v1beta1.Stack, owner client.Object) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
		labels, err := settings.GetMap(ctx, stack.Name, "gateway", "ingress", "labels")
		if err != nil {
			return err
		}
		if labels == nil {
			labels = map[string]string{}
		}
		labels["app.kubernetes.io/component"] = "gateway"
		labels["app.kubernetes.io/name"] = stack.Name
		t.SetLabels(labels)
		return nil
	}
}

func getAllHosts(ctx core.Context, gateway *v1beta1.Gateway) ([]string, error) {
	settingsHosts, err := settings.GetTrimmedStringSlice(ctx, gateway.Spec.Stack, "gateway", "ingress", "hosts")
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var hosts []string
	for _, h := range append(gateway.Spec.Ingress.GetHosts(), settingsHosts...) {
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func withTls(ctx core.Context, gateway *v1beta1.Gateway) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
		var secretName string
		if gateway.Spec.Ingress.TLS == nil {
			tlsEnabled, err := settings.GetBoolOrFalse(ctx, gateway.Spec.Stack, "gateway", "ingress", "tls", "enabled")
			if err != nil {
				return err
			}
			if !tlsEnabled {
				return nil
			}
			secretName = gateway.Name + "-tls"
		} else {
			secretName = gateway.Spec.Ingress.TLS.SecretName
		}

		hosts, err := getAllHosts(ctx, gateway)
		if err != nil {
			return err
		}

		t.Spec.TLS = []v1.IngressTLS{{
			SecretName: secretName,
			Hosts:      hosts,
		}}

		return nil
	}
}

func withIngressClassName(ctx core.Context, stack *v1beta1.Stack, gateway *v1beta1.Gateway) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
		ingressClassName, err := settings.GetString(ctx, stack.Name, "gateway", "ingress", "class")
		if err != nil {
			return err
		}

		if gateway.Spec.Ingress.IngressClassName != nil {
			t.Spec.IngressClassName = gateway.Spec.Ingress.IngressClassName
			return nil
		}

		if ingressClassName != nil {
			t.Spec.IngressClassName = ingressClassName
		}

		return nil
	}
}

func withIngressRules(ctx core.Context, gateway *v1beta1.Gateway) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
		hosts, err := getAllHosts(ctx, gateway)
		if err != nil {
			return err
		}

		pathType := v1.PathTypePrefix
		var rules []v1.IngressRule
		for _, host := range hosts {
			rules = append(rules, v1.IngressRule{
				Host: host,
				IngressRuleValue: v1.IngressRuleValue{
					HTTP: &v1.HTTPIngressRuleValue{
						Paths: []v1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &pathType,
								Backend: v1.IngressBackend{
									Service: &v1.IngressServiceBackend{
										Name: "gateway",
										Port: v1.ServiceBackendPort{
											Name: "http",
										},
									},
								},
							},
						},
					},
				},
			})
		}
		t.Spec.Rules = rules
		return nil
	}
}

func createIngress(ctx core.Context, stack *v1beta1.Stack,
	gateway *v1beta1.Gateway) error {
	name := types.NamespacedName{
		Namespace: stack.Name,
		Name:      "gateway",
	}
	if gateway.Spec.Ingress == nil {
		return core.DeleteIfExists[*v1.Ingress](ctx, name)
	}

	_, _, err := core.CreateOrUpdate(ctx, name,
		withAnnotations(ctx, stack, gateway),
		withLabels(ctx, stack, gateway),
		withIngressClassName(ctx, stack, gateway),
		withIngressRules(ctx, gateway),
		withTls(ctx, gateway),
		core.WithController[*v1.Ingress](ctx.GetScheme(), gateway),
	)

	return err
}
