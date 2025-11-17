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

package authclients

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/resourcereferences"
)

//+kubebuilder:rbac:groups=formance.com,resources=authclients,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=authclients/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=authclients/finalizers,verbs=update

func Reconcile(ctx Context, stack *v1beta1.Stack, authClient *v1beta1.AuthClient) error {
	if authClient.Spec.SecretFromSecret != nil && authClient.Spec.Secret != "" {
		return fmt.Errorf("cannot specify signing key using both .spec.SecretFromSecret and .spec.Secret fields")
	}

	resourceRefName := "client-secret"
	if authClient.Spec.SecretFromSecret != nil {
		ref, err := resourcereferences.Create(ctx, authClient, resourceRefName, authClient.Spec.SecretFromSecret.Name, &corev1.Secret{})
		if err != nil {
			return err
		}
		authClient.Status.Hash = ref.Status.Hash
	} else {
		if err := resourcereferences.Delete(ctx, authClient, resourceRefName); err != nil {
			return err
		}
		authClient.Status.Hash = ""
	}

	_, _, err := CreateOrUpdate[*corev1.Secret](ctx, types.NamespacedName{
		Name:      fmt.Sprintf("auth-client-%s", authClient.Name),
		Namespace: stack.Name,
	},
		func(t *corev1.Secret) error {
			t.StringData = map[string]string{
				"id":     authClient.Spec.ID,
				"secret": authClient.Spec.Secret,
			}

			return nil
		},
		WithController[*corev1.Secret](ctx.GetScheme(), authClient),
	)

	return err
}

func init() {
	Init(
		WithStackDependencyReconciler(Reconcile,
			WithOwn[*v1beta1.AuthClient](&corev1.Secret{}),
			WithOwn[*v1beta1.AuthClient](&v1beta1.ResourceReference{}),
		),
	)
}
