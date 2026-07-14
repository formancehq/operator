package gatewaygrpcapis

import (
	"k8s.io/apimachinery/pkg/types"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type option func(spec *v1beta1.GatewayGRPCAPI)

func Create(ctx core.Context, owner v1beta1.Module, options ...option) error {
	objectName := core.LowerCaseKind(ctx, owner)
	_, _, err := core.CreateOrUpdate[*v1beta1.GatewayGRPCAPI](ctx, types.NamespacedName{
		Name: core.GetObjectName(owner.GetStack(), core.LowerCaseKind(ctx, owner)),
	},
		func(t *v1beta1.GatewayGRPCAPI) error {
			t.Spec = v1beta1.GatewayGRPCAPISpec{
				StackDependency: v1beta1.StackDependency{
					Stack: owner.GetStack(),
				},
				Name: objectName,
			}
			for _, option := range options {
				option(t)
			}

			return nil
		},
		core.WithController[*v1beta1.GatewayGRPCAPI](ctx.GetScheme(), owner),
	)
	return err
}

func WithGRPCServices(services ...string) func(grpcapi *v1beta1.GatewayGRPCAPI) {
	return func(grpcapi *v1beta1.GatewayGRPCAPI) {
		grpcapi.Spec.GRPCServices = services
	}
}

func WithPort(port int32) func(grpcapi *v1beta1.GatewayGRPCAPI) {
	return func(grpcapi *v1beta1.GatewayGRPCAPI) {
		grpcapi.Spec.Port = port
	}
}
