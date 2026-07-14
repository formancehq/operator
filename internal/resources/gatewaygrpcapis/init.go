/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gatewaygrpcapis

import (
	corev1 "k8s.io/api/core/v1"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/services"
)

//+kubebuilder:rbac:groups=formance.com,resources=gatewaygrpcapis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=gatewaygrpcapis/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=gatewaygrpcapis/finalizers,verbs=update

func Reconcile(ctx Context, _ *v1beta1.Stack, grpcAPI *v1beta1.GatewayGRPCAPI) error {
	if grpcAPI.Spec.BackendRef != nil {
		return DeleteIfExists[*corev1.Service](ctx, GetNamespacedResourceName(grpcAPI.Spec.Stack, grpcAPI.Spec.Name+"-grpc"))
	}

	_, err := services.Create(ctx, grpcAPI, grpcAPI.Spec.Name+"-grpc",
		services.WithConfig(services.PortConfig{
			ServiceName: grpcAPI.Spec.Name,
			PortName:    "grpc",
			Port:        grpcAPI.Spec.Port,
			TargetPort:  "grpc",
		}),
	)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	Init(
		WithStackDependencyReconciler(Reconcile,
			WithOwn[*v1beta1.GatewayGRPCAPI](&corev1.Service{}),
			WithWatchSettings[*v1beta1.GatewayGRPCAPI](),
		),
	)
}
