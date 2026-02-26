package gatewayhttpapis

import (
	"github.com/iancoleman/strcase"
	"k8s.io/apimachinery/pkg/types"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type option func(spec *v1beta1.GatewayHTTPAPI)

var defaultOptions = []option{
	WithRules(RuleSecured()),
}

func Create(ctx core.Context, owner v1beta1.Module, options ...option) error {
	kind := owner.GetObjectKind().GroupVersionKind().Kind
	objectName := strcase.ToKebab(kind)
	_, _, err := core.CreateOrUpdate[*v1beta1.GatewayHTTPAPI](ctx, types.NamespacedName{
		Name: core.GetObjectName(owner.GetStack(), strcase.ToKebab(kind)),
	},
		func(t *v1beta1.GatewayHTTPAPI) error {
			t.Spec = v1beta1.GatewayHTTPAPISpec{
				StackDependency: v1beta1.StackDependency{
					Stack: owner.GetStack(),
				},
				Name: objectName,
			}
			for _, option := range append(defaultOptions, options...) {
				option(t)
			}

			return nil
		},
		core.WithController[*v1beta1.GatewayHTTPAPI](ctx.GetScheme(), owner),
	)
	return err
}

func WithRules(rules ...v1beta1.GatewayHTTPAPIRule) func(httpapi *v1beta1.GatewayHTTPAPI) {
	return func(httpapi *v1beta1.GatewayHTTPAPI) {
		httpapi.Spec.Rules = rules
	}
}

func WithHealthCheckEndpoint(v string) func(httpapi *v1beta1.GatewayHTTPAPI) {
	return func(httpapi *v1beta1.GatewayHTTPAPI) {
		httpapi.Spec.HealthCheckEndpoint = v
	}
}

func RuleSecured() v1beta1.GatewayHTTPAPIRule {
	return v1beta1.GatewayHTTPAPIRule{}
}

func RuleUnsecured() v1beta1.GatewayHTTPAPIRule {
	return v1beta1.GatewayHTTPAPIRule{
		Secured: true,
	}
}
