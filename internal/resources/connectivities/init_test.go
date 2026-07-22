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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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

// credsTestContext is a core.Context backed by a fake client + real scheme, for
// exercising ensureLedgerCredentials.
type credsTestContext struct {
	context.Context
	client client.Client
	scheme *runtime.Scheme
}

func (c credsTestContext) GetClient() client.Client    { return c.client }
func (c credsTestContext) GetScheme() *runtime.Scheme  { return c.scheme }
func (c credsTestContext) GetAPIReader() client.Reader { return c.client }
func (c credsTestContext) GetPlatform() core.Platform  { return core.Platform{} }

func newCredsTestContext(t *testing.T, objs ...client.Object) credsTestContext {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatalf("add v1beta1 to scheme: %v", err)
	}
	// Register the cluster-scoped ledger Credentials GVK so the fake client can
	// track it as an unstructured object.
	s.AddKnownTypeWithName(ledgerCredentialsGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(ledgerCredentialsGVK.GroupVersion().WithKind(ledgerCredentialsGVK.Kind+"List"), &unstructured.UnstructuredList{})
	return credsTestContext{
		Context: context.Background(),
		scheme:  s,
		client:  fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build(),
	}
}

func TestEnsureLedgerCredentialsCreatesGodCredentialAndReportsPending(t *testing.T) {
	ctx := newCredsTestContext(t)
	stack := &v1beta1.Stack{}
	stack.Name = "stack0"

	keyID, secret, ready, err := ensureLedgerCredentials(ctx, stack)
	if err != nil {
		t.Fatalf("ensureLedgerCredentials: %v", err)
	}
	if ready || keyID != "" || secret != "" {
		t.Fatalf("freshly created credential must be pending, got ready=%v keyID=%q secret=%q", ready, keyID, secret)
	}

	// The Credentials must have been created cluster-scoped with god + selector.
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ledgerCredentialsGVK)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: "connectivity-stack0"}, got); err != nil {
		t.Fatalf("Credentials was not created: %v", err)
	}
	if god, _, _ := unstructured.NestedBool(got.Object, "spec", "god"); !god {
		t.Error("Credentials must have spec.god=true")
	}
	if sel, _, _ := unstructured.NestedStringMap(got.Object, "spec", "selector", "matchLabels"); sel["formance.com/stack"] != "stack0" {
		t.Errorf("selector.matchLabels[formance.com/stack] = %q, want stack0", sel["formance.com/stack"])
	}
	if ns, _, _ := unstructured.NestedStringSlice(got.Object, "spec", "additionalNamespaces"); len(ns) != 1 || ns[0] != "stack0" {
		t.Errorf("additionalNamespaces = %v, want [stack0]", ns)
	}
}

func TestEnsureLedgerCredentialsReportsKeyAndSecretWhenReady(t *testing.T) {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(ledgerCredentialsGVK)
	existing.SetName("connectivity-stack1")
	_ = unstructured.SetNestedField(existing.Object, true, "spec", "god")
	_ = unstructured.SetNestedStringMap(existing.Object, map[string]string{"formance.com/stack": "stack1"}, "spec", "selector", "matchLabels")
	_ = unstructured.SetNestedStringSlice(existing.Object, []string{"stack1"}, "spec", "additionalNamespaces")
	_ = unstructured.SetNestedField(existing.Object, "Ready", "status", "phase")
	_ = unstructured.SetNestedField(existing.Object, "2ba721b2866686f6", "status", "keyID")
	_ = unstructured.SetNestedSlice(existing.Object, []any{
		map[string]any{"name": "ledger-connectivity-stack1-credentials-keys", "namespace": "stack1"},
	}, "status", "distributedSecretRefs")

	ctx := newCredsTestContext(t, existing)
	stack := &v1beta1.Stack{}
	stack.Name = "stack1"

	keyID, secret, ready, err := ensureLedgerCredentials(ctx, stack)
	if err != nil {
		t.Fatalf("ensureLedgerCredentials: %v", err)
	}
	if !ready {
		t.Fatal("credential with status Ready must report ready")
	}
	if keyID != "2ba721b2866686f6" {
		t.Errorf("keyID = %q, want 2ba721b2866686f6", keyID)
	}
	if secret != "ledger-connectivity-stack1-credentials-keys" {
		t.Errorf("secret = %q, want the distributed secret in the stack namespace", secret)
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
