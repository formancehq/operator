/*
Copyright 2026.

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

package mcps

import (
	"net/http"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/registries"
)

//+kubebuilder:rbac:groups=formance.com,resources=mcps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=mcps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=mcps/finalizers,verbs=update

const serviceName = "mcp"

func Reconcile(ctx Context, stack *v1beta1.Stack, mcp *v1beta1.MCP, version string) error {
	if err := gatewayhttpapis.Create(ctx, mcp,
		gatewayhttpapis.WithHealthCheckEndpoint("_healthcheck"),
		gatewayhttpapis.WithRules(
			v1beta1.GatewayHTTPAPIRule{
				Methods: []string{http.MethodPost},
			},
			v1beta1.GatewayHTTPAPIRule{
				Path:    "/.well-known/oauth-protected-resource",
				Methods: []string{http.MethodGet},
			},
			v1beta1.GatewayHTTPAPIRule{
				Path:    "/_healthcheck",
				Methods: []string{http.MethodGet},
			},
		)); err != nil {
		return err
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "stack-mcp", version)
	if err != nil {
		return err
	}

	return createDeployment(ctx, stack, mcp, imageConfiguration)
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			NoRequirements(),
			WithOwn[*v1beta1.MCP](&appsv1.Deployment{}),
			WithOwn[*v1beta1.MCP](&corev1.Service{}),
			WithOwn[*v1beta1.MCP](&v1beta1.GatewayHTTPAPI{}),
			WithWatchSettings[*v1beta1.MCP](),
			WithWatchDependency[*v1beta1.MCP](&v1beta1.Gateway{}),
		),
	)
}
