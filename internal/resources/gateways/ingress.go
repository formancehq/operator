package gateways

import (
	"strings"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func withAnnotations(ctx core.Context, stack *v1beta1.Stack, gateway *v1beta1.Gateway) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
		annotations, err := settings.GetMap(ctx, stack.Name, "gateway", "ingress", "annotations")
		if err != nil {
			return err
		}
		if len(annotations) > 0 {
			settings.LogDeprecation(ctx, stack.Name, "Gateway.Spec.Ingress.Annotations",
				"gateway", "ingress", "annotations")
		}
		if annotations == nil {
			annotations = map[string]string{}
		}
		// Spec annotations win on key collision.
		if gateway.Spec.Ingress.Annotations != nil {
			for k, v := range gateway.Spec.Ingress.Annotations {
				annotations[k] = v
			}
		}

		t.SetAnnotations(annotations)

		return nil
	}
}

func withLabels(ctx core.Context, stack *v1beta1.Stack, gateway *v1beta1.Gateway) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
		labels, err := settings.GetMap(ctx, stack.Name, "gateway", "ingress", "labels")
		if err != nil {
			return err
		}
		if len(labels) > 0 {
			settings.LogDeprecation(ctx, stack.Name, "Gateway.Spec.Ingress.Labels",
				"gateway", "ingress", "labels")
		}
		if labels == nil {
			labels = map[string]string{}
		}
		// Spec labels win on key collision.
		if gateway.Spec.Ingress != nil {
			for k, v := range gateway.Spec.Ingress.Labels {
				labels[k] = v
			}
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

	if len(settingsHosts) > 0 {
		settings.LogDeprecation(ctx, gateway.Spec.Stack, "Gateway.Spec.Ingress.Hosts",
			"gateway", "ingress", "hosts")
	}

	for i, h := range settingsHosts {
		settingsHosts[i] = strings.ReplaceAll(h, "{stack}", gateway.Spec.Stack)
	}

	return v1beta1.DedupHosts(append(gateway.Spec.Ingress.GetHosts(), settingsHosts...)), nil
}

func withTls(ctx core.Context, gateway *v1beta1.Gateway, hosts []string) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
		var secretName string
		if gateway.Spec.Ingress.TLS == nil {
			tlsEnabled, err := settings.GetBool(ctx, gateway.Spec.Stack, "gateway", "ingress", "tls", "enabled")
			if err != nil {
				return err
			}
			if tlsEnabled == nil || !*tlsEnabled {
				return nil
			}
			settings.LogDeprecation(ctx, gateway.Spec.Stack, "Gateway.Spec.Ingress.TLS",
				"gateway", "ingress", "tls", "enabled")
			secretName = gateway.Name + "-tls"
		} else {
			secretName = gateway.Spec.Ingress.TLS.SecretName
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
		var specClass string
		if gateway.Spec.Ingress.IngressClassName != nil {
			specClass = *gateway.Spec.Ingress.IngressClassName
		}
		ingressClassName, err := settings.PreferSpecString(ctx, stack.Name, specClass,
			"Gateway.Spec.Ingress.IngressClassName", "gateway", "ingress", "class")
		if err != nil {
			return err
		}

		if ingressClassName != "" {
			t.Spec.IngressClassName = &ingressClassName
		}

		return nil
	}
}

func withIngressRules(hosts []string) core.ObjectMutator[*v1.Ingress] {
	return func(t *v1.Ingress) error {
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

	hosts, err := getAllHosts(ctx, gateway)
	if err != nil {
		return err
	}

	_, _, err = core.CreateOrUpdate(ctx, name,
		withAnnotations(ctx, stack, gateway),
		withLabels(ctx, stack, gateway),
		withIngressClassName(ctx, stack, gateway),
		withIngressRules(hosts),
		withTls(ctx, gateway, hosts),
		core.WithController[*v1.Ingress](ctx.GetScheme(), gateway),
	)

	return err
}
