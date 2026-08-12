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

package gatewayhttpapis

import (
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/services"
)

//+kubebuilder:rbac:groups=formance.com,resources=gatewayhttpapis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=gatewayhttpapis/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=gatewayhttpapis/finalizers,verbs=update

func Reconcile(ctx Context, _ *v1beta1.Stack, httpAPI *v1beta1.GatewayHTTPAPI) error {
	// When every rule routes through an explicit backendRef, the gateway never
	// targets the default Service; leave the name free for the delegated
	// operator owning the backends (its own resources may legitimately claim it).
	if allRulesCarryBackendRef(httpAPI) {
		return deleteOwnedDefaultService(ctx, httpAPI)
	}

	_, err := services.Create(ctx, httpAPI, httpAPI.Spec.Name, services.WithDefault(httpAPI.Spec.Name))
	if err != nil {
		return err
	}

	return nil
}

func allRulesCarryBackendRef(httpAPI *v1beta1.GatewayHTTPAPI) bool {
	if len(httpAPI.Spec.Rules) == 0 {
		return false
	}
	for _, rule := range httpAPI.Spec.Rules {
		if rule.BackendRef == nil {
			return false
		}
	}
	return true
}

func deleteOwnedDefaultService(ctx Context, httpAPI *v1beta1.GatewayHTTPAPI) error {
	service := &corev1.Service{}
	err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Namespace: httpAPI.Spec.Stack,
		Name:      httpAPI.Spec.Name,
	}, service)
	if err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(service, httpAPI) {
		return nil
	}
	if err := ctx.GetClient().Delete(ctx, service); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func init() {
	Init(
		WithStackDependencyReconciler(Reconcile,
			WithOwn[*v1beta1.GatewayHTTPAPI](&corev1.Service{}),
			WithWatchSettings[*v1beta1.GatewayHTTPAPI](),
		),
	)
}
