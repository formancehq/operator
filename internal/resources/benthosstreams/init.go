/*
Copyright 2023.

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

package benthosstreams

import (
	"fmt"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

//+kubebuilder:rbac:groups=formance.com,resources=benthosstreams,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=formance.com,resources=benthosstreams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=benthosstreams/finalizers,verbs=update

func Reconcile(ctx Context, _ *v1beta1.Stack, stream *v1beta1.BenthosStream) error {

	// todo(next-minor): remove that cleanup code
	// clean legacy
	// since we don't create any BenthosStream in the operator
	// we can clean all streams owned by one of our component
	if len(stream.GetOwnerReferences()) == 1 && stream.GetOwnerReferences()[0].APIVersion == "formance.com/v1beta1" {
		return ctx.GetClient().Delete(ctx, stream)
	}

	cm, _, err := CreateOrUpdate[*corev1.ConfigMap](ctx, types.NamespacedName{
		Namespace: stream.Spec.Stack,
		Name:      fmt.Sprintf("stream-%s", stream.Name),
	},
		WithController[*corev1.ConfigMap](ctx.GetScheme(), stream),
		func(t *corev1.ConfigMap) error {
			t.Data = map[string]string{
				"stream.yaml": stream.Spec.Data,
			}

			return nil
		},
	)
	if err != nil {
		return err
	}

	stream.Status.ConfigMapHash = HashFromConfigMaps(cm)

	return nil
}

func init() {
	Init(
		WithStackDependencyReconciler(Reconcile,
			WithOwn[*v1beta1.BenthosStream](&corev1.ConfigMap{}),
		),
	)
}
