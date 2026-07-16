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

package connectivities

import (
	"context"
	"errors"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type connectivityDiscoveryContext struct {
	context.Context
	reader client.Reader
	client client.Client
}

func (c connectivityDiscoveryContext) GetClient() client.Client    { return c.client }
func (c connectivityDiscoveryContext) GetScheme() *runtime.Scheme  { return nil }
func (c connectivityDiscoveryContext) GetAPIReader() client.Reader { return c.reader }
func (c connectivityDiscoveryContext) GetPlatform() core.Platform  { return core.Platform{} }

// failingDiscoveryReader errors on every List, simulating a cluster where the
// operator cannot even list CRDs.
type failingDiscoveryReader struct {
	err error
}

func (r failingDiscoveryReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return r.err
}

func (r failingDiscoveryReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return r.err
}

// crdPresentReader reports the connectivity CRD as installed but makes the
// resource List fail, simulating a present-but-inaccessible dependency.
type crdPresentReader struct {
	resourceErr error
}

func (r crdPresentReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return nil
}

func (r crdPresentReader) List(_ context.Context, object client.ObjectList, _ ...client.ListOption) error {
	switch list := object.(type) {
	case *apiextensionsv1.CustomResourceDefinitionList:
		list.Items = []apiextensionsv1.CustomResourceDefinition{{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: connectivityGVK.Group,
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: connectivityGVK.Kind, Plural: "connectivities"},
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
					Name: connectivityGVK.Version, Served: true, Storage: true,
				}},
			},
		}}
		return nil
	case *unstructured.UnstructuredList:
		return r.resourceErr
	default:
		return nil
	}
}

// accessReviewClient answers SelfSubjectAccessReviews, denying a chosen verb.
type accessReviewClient struct {
	client.Client
	deniedVerb string
}

func (c accessReviewClient) Create(_ context.Context, object client.Object, _ ...client.CreateOption) error {
	review := object.(*authorizationv1.SelfSubjectAccessReview)
	review.Status.Allowed = review.Spec.ResourceAttributes.Verb != c.deniedVerb
	if !review.Status.Allowed {
		review.Status.Reason = "denied by test"
	}
	return nil
}

func TestConnectivityDiscoveryFailureDisablesCapabilityWithoutFailing(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{}
	withConnectivityClusterWatch()(&options)
	if len(options.Raws) != 1 {
		t.Fatalf("withConnectivityClusterWatch() registered %d raw builders, want 1", len(options.Raws))
	}

	ctx := connectivityDiscoveryContext{
		Context: context.Background(),
		reader:  failingDiscoveryReader{err: errors.New("CRD discovery forbidden")},
	}
	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("connectivity discovery failure must not fail controller setup: %v", err)
	}
	if connectivityAvailable {
		t.Fatal("connectivity capability remains enabled after discovery failure")
	}
}

func TestConnectivityResourceAccessFailureDisablesCapabilityWithoutFailing(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{}
	withConnectivityClusterWatch()(&options)

	ctx := connectivityDiscoveryContext{
		Context: context.Background(),
		reader:  crdPresentReader{resourceErr: errors.New("Connectivity list forbidden")},
	}
	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("connectivity resource access failure must not fail controller setup: %v", err)
	}
	if connectivityAvailable {
		t.Fatal("connectivity capability remains enabled when Connectivity objects are inaccessible")
	}
}

func TestConnectivityMissingPermissionDisablesCapabilityWithoutFailing(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{}
	withConnectivityClusterWatch()(&options)

	ctx := connectivityDiscoveryContext{
		Context: context.Background(),
		reader:  crdPresentReader{},
		client:  accessReviewClient{deniedVerb: "watch"},
	}
	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("connectivity partial RBAC must not fail controller setup: %v", err)
	}
	if connectivityAvailable {
		t.Fatal("connectivity capability remains enabled without watch permission")
	}
}

func TestConnectivityReconcilePendingWhenCapabilityUnavailable(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = false
	t.Cleanup(func() { connectivityAvailable = previous })

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"

	ctx := connectivityDiscoveryContext{Context: context.Background()}
	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if err == nil {
		t.Fatal("Reconcile() must return a pending error when the capability is unavailable")
	}
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if len(connectivity.Status.Conditions) == 0 {
		t.Fatal("Reconcile() must record a condition when the capability is unavailable")
	}
}
