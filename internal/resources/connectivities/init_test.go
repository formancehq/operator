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
	"reflect"
	"strconv"
	"strings"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
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

type credentialsCRDPresentReader struct {
	resourceErr error
}

func (r credentialsCRDPresentReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return nil
}

func (r credentialsCRDPresentReader) List(_ context.Context, object client.ObjectList, _ ...client.ListOption) error {
	switch list := object.(type) {
	case *apiextensionsv1.CustomResourceDefinitionList:
		list.Items = []apiextensionsv1.CustomResourceDefinition{{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: ledgerCredentialsGVK.Group,
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: ledgerCredentialsGVK.Kind, Plural: "credentials"},
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
					Name: ledgerCredentialsGVK.Version, Served: true, Storage: true,
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

func TestDeleteLedgerCredentialsFinalizerDeletesCredential(t *testing.T) {
	// A Connectivity being deleted (deletion timestamp + finalizer set) whose
	// god-mode ledger Credentials still exists cluster-scoped.
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(ledgerCredentialsGVK)
	existing.SetName("connectivity-stack0")
	_ = unstructured.SetNestedField(existing.Object, true, "spec", "god")

	ctx := newCredsTestContext(t, existing)
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"
	now := metav1.Now()
	connectivity.DeletionTimestamp = &now
	connectivity.Finalizers = []string{"delete-ledger-credentials"}

	if err := deleteLedgerCredentials(ctx, connectivity); err != nil {
		t.Fatalf("deleteLedgerCredentials: %v", err)
	}

	// The cluster-scoped Credentials must be gone once the finalizer ran.
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ledgerCredentialsGVK)
	err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: "connectivity-stack0"}, got)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Credentials must be deleted by the finalizer, got err=%v", err)
	}
}

func TestDisabledStackKeepsLedgerCredentialsWithoutReconcilingModule(t *testing.T) {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(ledgerCredentialsGVK)
	existing.SetName("connectivity-stack0")
	_ = unstructured.SetNestedField(existing.Object, true, "spec", "god")

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack0-uid")},
		Spec:       v1beta1.StackSpec{Disabled: true},
	}
	connectivity := &v1beta1.Connectivity{
		ObjectMeta: metav1.ObjectMeta{Name: "stack0"},
		Spec: v1beta1.ConnectivitySpec{
			StackDependency: v1beta1.StackDependency{Stack: stack.Name},
		},
	}
	ctx := newCredsTestContext(t, existing, connectivity)
	options := &core.ReconcilerOptions[*v1beta1.Connectivity]{
		Owns:     map[client.Object][]builder.OwnsOption{},
		Watchers: map[client.Object]core.ReconcilerOptionsWatch{},
	}
	for _, option := range connectivityReconcilerOptions() {
		option(options)
	}
	controller := core.ForModule(core.NoRequirements(), func(_ core.Context, _ *v1beta1.Stack, _ *core.ReconcilerOptions[*v1beta1.Connectivity], _ *v1beta1.Connectivity, _ string) error {
		t.Fatal("a disabled Stack must not reconcile its Connectivity module")
		return nil
	})

	if err := controller(ctx, stack, options, connectivity); err != nil {
		t.Fatalf("disable Connectivity module: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ledgerCredentialsGVK)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: existing.GetName()}, got); err != nil {
		t.Fatalf("Stack disable must keep ledger Credentials for re-enable: %v", err)
	}
}

func TestDeleteLedgerCredentialsFinalizerIsNoOpWhenAlreadyGone(t *testing.T) {
	// No Credentials seeded: the finalizer must be idempotent and not error when
	// the credential has already been removed.
	ctx := newCredsTestContext(t)
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"
	now := metav1.Now()
	connectivity.DeletionTimestamp = &now
	connectivity.Finalizers = []string{"delete-ledger-credentials"}

	if err := deleteLedgerCredentials(ctx, connectivity); err != nil {
		t.Fatalf("deleteLedgerCredentials on missing credential must be a no-op, got: %v", err)
	}
}

func TestDeleteLedgerCredentialsFinalizerToleratesMissingCRD(t *testing.T) {
	// Without the ledger CRD, Delete returns a no-match error; the finalizer must
	// still complete or the Connectivity would be stuck terminating forever.
	ctx := credsTestContext{Context: context.Background(), client: noMatchDeleteClient{}}
	connectivity := &v1beta1.Connectivity{}
	connectivity.Spec.Stack = "stack0"
	if err := deleteLedgerCredentials(ctx, connectivity); err != nil {
		t.Fatalf("finalizer must tolerate a missing ledger CRD, got %v", err)
	}
}

type noMatchDeleteClient struct{ client.Client }

func (noMatchDeleteClient) Delete(context.Context, client.Object, ...client.DeleteOption) error {
	return &apimeta.NoKindMatchError{GroupKind: ledgerCredentialsGVK.GroupKind()}
}

func TestConnectivityMonitoringSpecNilWhenDisabled(t *testing.T) {
	if got := connectivityMonitoringSpec(nil); got != nil {
		t.Fatalf("connectivityMonitoringSpec(nil) = %v, want nil (telemetry disabled)", got)
	}
}

func TestApplyConnectivityMonitoringEmbedsSignalsInline(t *testing.T) {
	cfg := &settings.OpenTelemetryConfiguration{
		// ServiceName is intentionally ignored: the operator forces "connectivity".
		ServiceName: "ignored",
		Attributes:  map[string]string{"stack": "stack0", "pod-name": "$(POD_NAME)"},
		Traces:      &settings.OpenTelemetrySignalConfiguration{Endpoint: "otel-collector.stack0", Port: "4318", Insecure: true, Mode: "http"},
		Metrics:     &settings.OpenTelemetrySignalConfiguration{Endpoint: "otel-collector.stack0", Port: "4318", Insecure: true, Mode: "http"},
		Logs:        &settings.OpenTelemetrySignalConfiguration{Endpoint: "otel-collector.stack0", Port: "4318", Insecure: true, Mode: "http"},
	}

	object := &unstructured.Unstructured{Object: map[string]any{}}
	if err := applyConnectivityMonitoring(object, cfg); err != nil {
		t.Fatalf("applyConnectivityMonitoring: %v", err)
	}

	if sn, _, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "serviceName"); sn != "connectivity" {
		t.Errorf("serviceName = %q, want connectivity", sn)
	}
	// pod-name=$(POD_NAME) is dropped: the connectivity operator forwards
	// OTEL_RESOURCE_ATTRIBUTES verbatim without defining a POD_NAME env var, so
	// the placeholder would surface literally. Only resolvable attributes remain.
	if attrs, _, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "attributes"); attrs != "stack=stack0" {
		t.Errorf("attributes = %q, want sorted key=value list without unresolvable placeholders", attrs)
	}

	if enabled, _, _ := unstructured.NestedBool(object.Object, "spec", "monitoring", "traces", "enabled"); !enabled {
		t.Error("traces.enabled must be true")
	}
	if exporter, _, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "traces", "exporter"); exporter != "otlp" {
		t.Errorf("traces.exporter = %q, want otlp", exporter)
	}
	if batch, _, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "traces", "batch"); batch != "true" {
		t.Errorf("traces.batch = %q, want \"true\"", batch)
	}
	if insecure, _, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "traces", "insecure"); insecure != "true" {
		t.Errorf("traces.insecure = %q, want \"true\" (string)", insecure)
	}

	if endpoint, _, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "metrics", "endpoint"); endpoint != "otel-collector.stack0" {
		t.Errorf("metrics.endpoint = %q, want otel-collector.stack0", endpoint)
	}
	if runtimeOn, _, _ := unstructured.NestedBool(object.Object, "spec", "monitoring", "metrics", "runtime"); !runtimeOn {
		t.Error("metrics.runtime must be true")
	}

	// Logs is configured but, unlike traces/metrics, carries no batch/runtime
	// extra — just the enabled otlp signal.
	if enabled, _, _ := unstructured.NestedBool(object.Object, "spec", "monitoring", "logs", "enabled"); !enabled {
		t.Error("logs.enabled must be true")
	}
	if mode, _, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "logs", "mode"); mode != "http" {
		t.Errorf("logs.mode = %q, want http", mode)
	}
}

func TestConnectivityMonitoringSpecDropsUnresolvableAttributes(t *testing.T) {
	// The connectivity operator emits OTEL_RESOURCE_ATTRIBUTES verbatim and
	// defines no POD_NAME env var, so a forwarded $(POD_NAME) would surface
	// literally. Such placeholders must be stripped, keeping literal attributes.
	got := connectivityMonitoringSpec(&settings.OpenTelemetryConfiguration{
		Attributes: map[string]string{
			"stack":    "stack0",
			"pod-name": "$(POD_NAME)",
			"team":     "connectivity",
		},
	})
	if attrs := got["attributes"]; attrs != "stack=stack0,team=connectivity" {
		t.Errorf("attributes = %q, want the placeholder stripped and literals kept, sorted", attrs)
	}

	// When the only attribute is an unresolvable placeholder, the attributes key
	// must be omitted entirely rather than set to an empty string.
	onlyPlaceholder := connectivityMonitoringSpec(&settings.OpenTelemetryConfiguration{
		Attributes: map[string]string{"pod-name": "$(POD_NAME)"},
	})
	if _, found := onlyPlaceholder["attributes"]; found {
		t.Errorf("attributes must be omitted when every attribute is unresolvable, got %v", onlyPlaceholder["attributes"])
	}
}

func TestApplyConnectivityMonitoringOmitsUnconfiguredSignals(t *testing.T) {
	// Only metrics enabled: traces and logs blocks must be absent.
	cfg := &settings.OpenTelemetryConfiguration{
		Metrics: &settings.OpenTelemetrySignalConfiguration{Endpoint: "otel-collector.stack0", Port: "4318", Mode: "http"},
	}
	object := &unstructured.Unstructured{Object: map[string]any{}}
	if err := applyConnectivityMonitoring(object, cfg); err != nil {
		t.Fatalf("applyConnectivityMonitoring: %v", err)
	}
	if _, found, _ := unstructured.NestedMap(object.Object, "spec", "monitoring", "traces"); found {
		t.Error("spec.monitoring.traces must be absent when traces are not configured")
	}
	if _, found, _ := unstructured.NestedMap(object.Object, "spec", "monitoring", "logs"); found {
		t.Error("spec.monitoring.logs must be absent when logs are not configured")
	}
	// No resource attributes were provided, so the attributes key must be omitted.
	if _, found, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "attributes"); found {
		t.Error("spec.monitoring.attributes must be omitted when no attributes are set")
	}
}

func TestApplyConnectivityMonitoringIsIdempotent(t *testing.T) {
	cfg := &settings.OpenTelemetryConfiguration{
		Attributes: map[string]string{"stack": "stack0"},
		Traces:     &settings.OpenTelemetrySignalConfiguration{Endpoint: "otel-collector.stack0", Port: "4318", Insecure: false, Mode: "grpc"},
	}

	first := &unstructured.Unstructured{Object: map[string]any{}}
	if err := applyConnectivityMonitoring(first, cfg); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	// Re-applying the same configuration onto the already-populated object must
	// yield a byte-for-byte identical spec (idempotent reconcile).
	second := first.DeepCopy()
	if err := applyConnectivityMonitoring(second, cfg); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !reflect.DeepEqual(first.Object, second.Object) {
		t.Errorf("monitoring is not idempotent:\nfirst  = %#v\nsecond = %#v", first.Object, second.Object)
	}
}

func TestApplyConnectivityMonitoringPrunesStaleBlockWhenDisabled(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{}}
	// Seed a stale monitoring block, as if telemetry had been enabled before.
	if err := unstructured.SetNestedField(object.Object, "connectivity", "spec", "monitoring", "serviceName"); err != nil {
		t.Fatalf("seed stale monitoring: %v", err)
	}

	if err := applyConnectivityMonitoring(object, nil); err != nil {
		t.Fatalf("applyConnectivityMonitoring(nil): %v", err)
	}
	if _, found, _ := unstructured.NestedMap(object.Object, "spec", "monitoring"); found {
		t.Error("spec.monitoring must be pruned when telemetry is disabled")
	}
}

// newConnectivityMapContext builds a core.Context backed by a fake client that
// indexes Connectivity by stack (as the manager does at runtime), for exercising
// the ledger Credentials -> Connectivity watch mapping.
func newConnectivityMapContext(t *testing.T, objs ...client.Object) credsTestContext {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatalf("add v1beta1 to scheme: %v", err)
	}
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithIndex(&v1beta1.Connectivity{}, "stack", func(obj client.Object) []string {
			return []string{obj.(*v1beta1.Connectivity).GetStack()}
		}).
		WithObjects(objs...).
		Build()
	return credsTestContext{Context: context.Background(), scheme: s, client: c}
}

func newConnectivity(name, stack string) *v1beta1.Connectivity {
	c := &v1beta1.Connectivity{}
	c.Name = name
	c.Spec.Stack = stack
	return c
}

func newLedgerCredentials(name string) *unstructured.Unstructured {
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(ledgerCredentialsGVK)
	cred.SetName(name)
	return cred
}

func TestMapLedgerCredentialsToConnectivityEnqueuesMatchingConnectivity(t *testing.T) {
	// One Connectivity per stack; only the one in the Credentials' stack must be
	// enqueued.
	ctx := newConnectivityMapContext(t,
		newConnectivity("stack1", "stack1"),
		newConnectivity("stack2", "stack2"),
	)

	requests := mapLedgerCredentialsToConnectivity(ctx, newLedgerCredentials("connectivity-stack1"))
	if len(requests) != 1 {
		t.Fatalf("expected exactly one reconcile request, got %d: %v", len(requests), requests)
	}
	if requests[0].Name != "stack1" {
		t.Errorf("enqueued Connectivity %q, want stack1", requests[0].Name)
	}
}

func TestMapLedgerCredentialsToConnectivityReturnsNothingWithoutMatchingConnectivity(t *testing.T) {
	// A Connectivity exists, but not in the stack the Credentials belongs to.
	ctx := newConnectivityMapContext(t, newConnectivity("stack1", "stack1"))

	if requests := mapLedgerCredentialsToConnectivity(ctx, newLedgerCredentials("connectivity-stack9")); len(requests) != 0 {
		t.Fatalf("expected no reconcile requests for a stack without Connectivity, got %v", requests)
	}
}

func TestMapLedgerCredentialsToConnectivityIgnoresForeignCredentials(t *testing.T) {
	// A ledger Credentials not provisioned by the connectivity module (no
	// "connectivity-" name prefix) must never enqueue a Connectivity, even when a
	// Connectivity would otherwise match by stack.
	ctx := newConnectivityMapContext(t, newConnectivity("stack1", "stack1"))

	if requests := mapLedgerCredentialsToConnectivity(ctx, newLedgerCredentials("some-other-credential")); requests != nil {
		t.Fatalf("foreign Credentials must map to no Connectivity, got %v", requests)
	}
	// The bare prefix with an empty stack must also be rejected.
	if _, ok := connectivityStackFromCredentials(newLedgerCredentials("connectivity-")); ok {
		t.Fatal("a Credentials named exactly \"connectivity-\" must not resolve a stack")
	}
}

func TestLedgerCredentialsWatchDisabledWhenCRDAbsent(t *testing.T) {
	previous := ledgerCredentialsWatchAvailable
	ledgerCredentialsWatchAvailable = true
	t.Cleanup(func() { ledgerCredentialsWatchAvailable = previous })

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{}
	withLedgerCredentialsWatch()(&options)
	if len(options.Raws) != 1 {
		t.Fatalf("withLedgerCredentialsWatch() registered %d raw builders, want 1", len(options.Raws))
	}

	// crdPresentReader advertises only the connectivity CRD, not the ledger
	// Credentials CRD, so the watch must stay unregistered without touching the
	// (nil) builder or failing setup.
	ctx := connectivityDiscoveryContext{Context: context.Background(), reader: crdPresentReader{}}
	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("absent ledger Credentials CRD must not fail controller setup: %v", err)
	}
	if ledgerCredentialsWatchAvailable {
		t.Fatal("ledger Credentials watch remains enabled when the CRD is absent")
	}
}

func TestLedgerCredentialsWatchDisabledWhenDiscoveryFails(t *testing.T) {
	previous := ledgerCredentialsWatchAvailable
	ledgerCredentialsWatchAvailable = true
	t.Cleanup(func() { ledgerCredentialsWatchAvailable = previous })

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{}
	withLedgerCredentialsWatch()(&options)

	ctx := connectivityDiscoveryContext{
		Context: context.Background(),
		reader:  failingDiscoveryReader{err: errors.New("CRD discovery forbidden")},
	}
	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("discovery failure must not fail controller setup: %v", err)
	}
	if ledgerCredentialsWatchAvailable {
		t.Fatal("ledger Credentials watch remains enabled after discovery failure")
	}
}

func TestLedgerCredentialsWatchDisabledWhenCredentialsListIsForbidden(t *testing.T) {
	previous := ledgerCredentialsWatchAvailable
	ledgerCredentialsWatchAvailable = true
	t.Cleanup(func() { ledgerCredentialsWatchAvailable = previous })

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{}
	withLedgerCredentialsWatch()(&options)
	ctx := connectivityDiscoveryContext{
		Context: context.Background(),
		reader: credentialsCRDPresentReader{resourceErr: apierrors.NewForbidden(
			schema.GroupResource{Group: ledgerCredentialsGVK.Group, Resource: "credentials"},
			"", errors.New("forbidden by test"))},
		client: accessReviewClient{},
	}

	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("inaccessible ledger Credentials must not fail controller setup: %v", err)
	}
	if ledgerCredentialsWatchAvailable {
		t.Fatal("ledger Credentials watch remains enabled when Credentials cannot be listed")
	}
}

func TestLedgerCredentialsWatchDisabledWithoutWatchPermission(t *testing.T) {
	previous := ledgerCredentialsWatchAvailable
	ledgerCredentialsWatchAvailable = true
	t.Cleanup(func() { ledgerCredentialsWatchAvailable = previous })

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{}
	withLedgerCredentialsWatch()(&options)
	ctx := connectivityDiscoveryContext{
		Context: context.Background(),
		reader:  credentialsCRDPresentReader{},
		client:  accessReviewClient{deniedVerb: "watch"},
	}

	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("missing ledger Credentials watch RBAC must not fail controller setup: %v", err)
	}
	if ledgerCredentialsWatchAvailable {
		t.Fatal("ledger Credentials watch remains enabled without watch permission")
	}
}

// newReconcileTestContext builds a core.Context backed by a fake client whose
// scheme knows the v1beta1 types, the delegated connectivity GVK and the ledger
// Credentials GVK (as unstructured), and whose client indexes Ledgers by their
// "stack" field so getStackLedger's List(MatchingFields{"stack": ...}) resolves.
func newReconcileTestContext(t *testing.T, objs ...client.Object) credsTestContext {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatalf("add v1beta1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add core v1 to scheme: %v", err)
	}
	s.AddKnownTypeWithName(connectivityGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(connectivityGVK.GroupVersion().WithKind(connectivityGVK.Kind+"List"), &unstructured.UnstructuredList{})
	s.AddKnownTypeWithName(ledgerCredentialsGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(ledgerCredentialsGVK.GroupVersion().WithKind(ledgerCredentialsGVK.Kind+"List"), &unstructured.UnstructuredList{})
	return credsTestContext{
		Context: context.Background(),
		scheme:  s,
		client: fake.NewClientBuilder().WithScheme(s).
			// The requirements framework (GetSingleDependency) lists Ledgers as
			// unstructured, so the fake index must serve both shapes.
			WithIndex(&v1beta1.Ledger{}, "stack", func(obj client.Object) []string {
				switch ledger := obj.(type) {
				case *v1beta1.Ledger:
					return []string{ledger.Spec.Stack}
				case *unstructured.Unstructured:
					stackName, _, _ := unstructured.NestedString(ledger.Object, "spec", "stack")
					return []string{stackName}
				default:
					return nil
				}
			}).
			WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
				return obj.(*v1beta1.Settings).GetStacks()
			}).
			WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
				return []string{strconv.Itoa(len(strings.Split(obj.(*v1beta1.Settings).Spec.Key, ".")))}
			}).
			WithIndex(&v1beta1.LedgerConfiguration{}, "stack", func(obj client.Object) []string {
				return obj.(*v1beta1.LedgerConfiguration).GetStacks()
			}).
			WithObjects(objs...).Build(),
	}
}

// newDelegatedConnectivity returns the delegated Connectivity resource as it is
// provisioned by Reconcile: namespaced in the stack namespace, with the fixed
// name "connectivity".
func newDelegatedConnectivity(stackName string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(connectivityGVK)
	object.SetNamespace(stackName)
	object.SetName(connectivityDelegatedName)
	return object
}

// delegatedConnectivityExists reports whether the delegated Connectivity for the
// stack is still present in the cluster.
func delegatedConnectivityExists(t *testing.T, ctx credsTestContext, stackName string) bool {
	t.Helper()
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(connectivityGVK)
	err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stackName, Name: connectivityDelegatedName}, got)
	return err == nil
}

// gatewayHTTPAPIExists reports whether the connectivity GatewayHTTPAPI
// ("<stack>-connectivity", cluster-scoped) is still present in the cluster.
func gatewayHTTPAPIExists(t *testing.T, ctx credsTestContext, stackName string) bool {
	t.Helper()
	got := &v1beta1.GatewayHTTPAPI{}
	err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: stackName + "-connectivity"}, got)
	return err == nil
}

// newLedgerCredentialsForStack returns the cluster-scoped god-mode Credentials
// as ensureLedgerCredentials provisions it: named "connectivity-<stack>".
func newLedgerCredentialsForStack(stackName string) *unstructured.Unstructured {
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(ledgerCredentialsGVK)
	cred.SetName("connectivity-" + stackName)
	return cred
}

// credentialsExist reports whether the cluster-scoped god-mode Credentials for
// the stack is still present in the cluster.
func credentialsExist(t *testing.T, ctx credsTestContext, stackName string) bool {
	t.Helper()
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ledgerCredentialsGVK)
	err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: "connectivity-" + stackName}, got)
	return err == nil
}

func TestConnectivityReconcileTearsDownDelegatedWhenLedgerNotV3(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	// A v2 (non-v3) ledger: a real downgrade below the connectivity prerequisite.
	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v2.0.0"
	ledger.Status.Ready = true

	// Pre-provisioned delegated resources, as if connectivity had been running
	// before the ledger was downgraded.
	delegated := newDelegatedConnectivity("stack0")
	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.Name = "stack0-connectivity"
	cred := newLedgerCredentialsForStack("stack0")

	ctx := newReconcileTestContext(t, ledger, delegated, httpAPI, cred)

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if err == nil {
		t.Fatal("Reconcile() must return a pending error when the ledger is not v3")
	}
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if len(connectivity.Status.Conditions) == 0 || connectivity.Status.Conditions[0].Reason != "LedgerNotV3" {
		t.Fatalf("expected a LedgerNotV3 condition, got %#v", connectivity.Status.Conditions)
	}

	if delegatedConnectivityExists(t, ctx, "stack0") {
		t.Error("delegated Connectivity must be torn down when the ledger is not v3")
	}
	if gatewayHTTPAPIExists(t, ctx, "stack0") {
		t.Error("GatewayHTTPAPI must be torn down when the ledger is not v3")
	}
	if credentialsExist(t, ctx, "stack0") {
		t.Error("god-mode Credentials must be torn down when the ledger is not v3")
	}
}

// stubPreviewActive stubs the preview Setting lookup (whose real
// implementation depends on the ledger controller's startup capability
// discovery) for the duration of a test. The preview logic itself is covered
// by the ledgers package tests.
func stubPreviewActive(t *testing.T, active bool, err error) {
	t.Helper()
	previous := ledgerV3PreviewActive
	ledgerV3PreviewActive = func(core.Context, *v1beta1.Stack) (bool, error) {
		return active, err
	}
	t.Cleanup(func() { ledgerV3PreviewActive = previous })
}

// newPreviewLedger returns a v2 ledger for the stack, optionally carrying the
// LedgerV3PreviewReady condition the ledger reconciler stamps once the preview
// Cluster runs.
func newPreviewLedger(stackName string, previewReady bool) *v1beta1.Ledger {
	ledger := &v1beta1.Ledger{}
	ledger.Name = stackName + "-ledger"
	ledger.Spec.Stack = stackName
	ledger.Spec.Version = "v2.0.0"
	ledger.Status.Ready = true
	if previewReady {
		ledger.Status.Conditions = v1beta1.Conditions{{
			Type:   "LedgerV3PreviewReady",
			Status: metav1.ConditionTrue,
		}}
	}
	return ledger
}

// A v2 ledger with the v3 preview active and reconciled to Ready satisfies the
// connectivity prerequisite: the gate must pass and the delegated resources
// must not be torn down, with the reconcile proceeding to the credentials
// provisioning.
func TestConnectivityReconcilePassesGateWhenLedgerV3PreviewActive(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	stubPreviewActive(t, true, nil)

	delegated := newDelegatedConnectivity("stack0")
	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.Name = "stack0-connectivity"

	ctx := newReconcileTestContext(t, newPreviewLedger("stack0", true), delegated, httpAPI)

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending on the freshly created credentials", err)
	}
	if len(connectivity.Status.Conditions) == 0 || connectivity.Status.Conditions[0].Reason != "LedgerCredentialsPending" {
		t.Fatalf("expected the gate to pass through to LedgerCredentialsPending, got %#v", connectivity.Status.Conditions)
	}

	if !delegatedConnectivityExists(t, ctx, "stack0") {
		t.Error("delegated Connectivity must NOT be torn down while the v3 preview is active")
	}
	if !gatewayHTTPAPIExists(t, ctx, "stack0") {
		t.Error("GatewayHTTPAPI must NOT be torn down while the v3 preview is active")
	}
	if !credentialsExist(t, ctx, "stack0") {
		t.Error("god-mode Credentials must have been provisioned while the v3 preview is active")
	}
}

// A preview Setting that the ledger reconciler has not observed yet leaves the
// Ledger status stale: Ready=true from the v2-only reconcile, no
// LedgerV3PreviewReady condition. Initial provisioning must NOT happen against
// the not-yet-existing v3 service — and since the god-mode Credentials turns
// Ready from additionalNamespaces alone, the credentials gate cannot be relied
// upon to block it. Already-provisioned resources are retained (transient).
func TestConnectivityReconcileBlocksProvisioningUntilPreviewReady(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	stubPreviewActive(t, true, nil)

	t.Run("initial provisioning is blocked", func(t *testing.T) {
		ctx := newReconcileTestContext(t, newPreviewLedger("stack0", false))

		stack := &v1beta1.Stack{}
		stack.Name = "stack0"
		connectivity := &v1beta1.Connectivity{}
		connectivity.Name = "stack0"
		connectivity.Spec.Stack = "stack0"

		err := Reconcile(ctx, stack, connectivity, "v1.0.0")
		if !core.IsApplicationError(err) {
			t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
		}
		if len(connectivity.Status.Conditions) == 0 || connectivity.Status.Conditions[0].Reason != "LedgerV3PreviewNotReady" {
			t.Fatalf("expected a LedgerV3PreviewNotReady condition, got %#v", connectivity.Status.Conditions)
		}
		if delegatedConnectivityExists(t, ctx, "stack0") {
			t.Error("delegated Connectivity must NOT be provisioned before the preview is reconciled to Ready")
		}
		if gatewayHTTPAPIExists(t, ctx, "stack0") {
			t.Error("GatewayHTTPAPI must NOT be provisioned before the preview is reconciled to Ready")
		}
	})

	t.Run("existing resources are retained", func(t *testing.T) {
		delegated := newDelegatedConnectivity("stack0")
		httpAPI := &v1beta1.GatewayHTTPAPI{}
		httpAPI.Name = "stack0-connectivity"
		cred := newLedgerCredentialsForStack("stack0")
		ctx := newReconcileTestContext(t, newPreviewLedger("stack0", false), delegated, httpAPI, cred)

		stack := &v1beta1.Stack{}
		stack.Name = "stack0"
		connectivity := &v1beta1.Connectivity{}
		connectivity.Name = "stack0"
		connectivity.Spec.Stack = "stack0"

		if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
			t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
		}
		if !delegatedConnectivityExists(t, ctx, "stack0") {
			t.Error("delegated Connectivity must NOT be torn down while the preview is transiently not ready")
		}
		if !gatewayHTTPAPIExists(t, ctx, "stack0") {
			t.Error("GatewayHTTPAPI must NOT be torn down while the preview is transiently not ready")
		}
		if !credentialsExist(t, ctx, "stack0") {
			t.Error("god-mode Credentials must NOT be torn down while the preview is transiently not ready")
		}
	})
}

// An error resolving the v3 preview Setting is transient (like an unresolvable
// module version): the reconcile must stay pending without tearing down the
// delegated resources — and must poll, because a Settings read failure
// produces no watch event on recovery.
func TestConnectivityReconcileKeepsDelegatedWhenPreviewGateUnresolved(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	stubPreviewActive(t, false, errors.New("cannot read the preview Setting"))

	delegated := newDelegatedConnectivity("stack0")
	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.Name = "stack0-connectivity"
	cred := newLedgerCredentialsForStack("stack0")

	ctx := newReconcileTestContext(t, newPreviewLedger("stack0", true), delegated, httpAPI, cred)

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if core.ApplicationErrorRequeueAfter(err) <= 0 {
		t.Fatalf("an unresolved preview Setting must request a delayed requeue (no watch event fires on recovery), got %v", err)
	}
	if len(connectivity.Status.Conditions) == 0 || connectivity.Status.Conditions[0].Reason != "LedgerV3PreviewUnresolved" {
		t.Fatalf("expected a LedgerV3PreviewUnresolved condition, got %#v", connectivity.Status.Conditions)
	}

	if !delegatedConnectivityExists(t, ctx, "stack0") {
		t.Error("delegated Connectivity must NOT be torn down on a transient preview resolution error")
	}
	if !gatewayHTTPAPIExists(t, ctx, "stack0") {
		t.Error("GatewayHTTPAPI must NOT be torn down on a transient preview resolution error")
	}
	if !credentialsExist(t, ctx, "stack0") {
		t.Error("god-mode Credentials must NOT be torn down on a transient preview resolution error")
	}
}

// The module registration must let a v2-with-preview stack through to the
// Reconcile gate: a VersionAtLeast(LedgerV3Version) requirement would fail
// with DependencyVersionMismatch in ForModule before Reconcile could consult
// the preview Setting. Exercise the full module-controller path with the real
// registration's requirements.
func TestConnectivityModuleControllerReachesGateOnPreviewStack(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	stubPreviewActive(t, true, nil)

	stack := &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack0-uid")},
		Spec:       v1beta1.StackSpec{Version: "v2.0.0"},
	}
	connectivity := &v1beta1.Connectivity{
		ObjectMeta: metav1.ObjectMeta{Name: "stack0"},
		Spec: v1beta1.ConnectivitySpec{
			StackDependency: v1beta1.StackDependency{Stack: stack.Name},
		},
	}
	ctx := newReconcileTestContext(t, newPreviewLedger("stack0", false), stack, connectivity)

	options := &core.ReconcilerOptions[*v1beta1.Connectivity]{
		Owns:     map[client.Object][]builder.OwnsOption{},
		Watchers: map[client.Object]core.ReconcilerOptionsWatch{},
	}
	for _, option := range connectivityReconcilerOptions() {
		option(options)
	}
	controller := core.ForModule(connectivityModuleRequirements(),
		func(ctx core.Context, stack *v1beta1.Stack, _ *core.ReconcilerOptions[*v1beta1.Connectivity], connectivity *v1beta1.Connectivity, version string) error {
			return Reconcile(ctx, stack, connectivity, version)
		})

	err := controller(ctx, stack, options, connectivity)
	if !core.IsApplicationError(err) {
		t.Fatalf("module controller returned %v, want the module gate's pending error", err)
	}

	dependencies := connectivity.GetConditions().Get("DependenciesSatisfied")
	if dependencies == nil || dependencies.Status != metav1.ConditionTrue {
		t.Fatalf("the presence-only Ledger requirement must be satisfied by a v2 ledger, got %#v", dependencies)
	}
	gate := connectivity.GetConditions().Get(connectivityReadyCondition)
	if gate == nil || gate.Reason != "LedgerV3PreviewNotReady" {
		t.Fatalf("Reconcile's preview gate must be reached on a v2-with-preview stack, got %#v", connectivity.Status.Conditions)
	}
}

func TestConnectivityReconcileKeepsDelegatedWhenLedgerV3NotReady(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	// A v3 ledger that is momentarily not ready: the prerequisite still holds, so
	// this is a transient gate that must NOT flap the workload.
	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = false

	delegated := newDelegatedConnectivity("stack0")
	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.Name = "stack0-connectivity"
	cred := newLedgerCredentialsForStack("stack0")

	ctx := newReconcileTestContext(t, ledger, delegated, httpAPI, cred)

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if err == nil {
		t.Fatal("Reconcile() must return a pending error when the ledger is not ready")
	}
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if len(connectivity.Status.Conditions) == 0 || connectivity.Status.Conditions[0].Reason != "LedgerNotReady" {
		t.Fatalf("expected a LedgerNotReady condition, got %#v", connectivity.Status.Conditions)
	}

	if !delegatedConnectivityExists(t, ctx, "stack0") {
		t.Error("delegated Connectivity must NOT be torn down on a transient ledger-not-ready gate")
	}
	if !gatewayHTTPAPIExists(t, ctx, "stack0") {
		t.Error("GatewayHTTPAPI must NOT be torn down on a transient ledger-not-ready gate")
	}
	if !credentialsExist(t, ctx, "stack0") {
		t.Error("god-mode Credentials must NOT be torn down on a transient ledger-not-ready gate")
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
	connectivity.Spec.Stack = "stack0"

	ctx := newReconcileTestContext(t)
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

// When the connectivity operator becomes unavailable after resources were
// provisioned AND the ledger hard gate has since closed, the capability
// short-circuit must not skip the teardown: the gateway route and god-mode
// Credentials still have to be removed.
func TestConnectivityReconcileTearsDownWhenCapabilityUnavailableAndLedgerGateClosed(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = false
	t.Cleanup(func() { connectivityAvailable = previous })

	// No ledger on the stack: the hard gate is closed.
	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.Name = "stack0-connectivity"
	cred := newLedgerCredentialsForStack("stack0")

	ctx := newReconcileTestContext(t, httpAPI, cred)

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if gatewayHTTPAPIExists(t, ctx, "stack0") {
		t.Error("GatewayHTTPAPI must be torn down when the capability is unavailable and the ledger gate is closed")
	}
	if credentialsExist(t, ctx, "stack0") {
		t.Error("god-mode Credentials must be torn down when the capability is unavailable and the ledger gate is closed")
	}
}

func TestConnectivityReconcileStaysPendingWhenUnavailableDelegatedDeleteIsForbidden(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = false
	t.Cleanup(func() { connectivityAvailable = previous })

	delegated := newDelegatedConnectivity("stack0")
	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.Name = "stack0-connectivity"
	cred := newLedgerCredentialsForStack("stack0")
	base := newReconcileTestContext(t, delegated, httpAPI, cred)
	forbidden := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetObjectKind().GroupVersionKind() == connectivityGVK {
				return apierrors.NewForbidden(
					schema.GroupResource{Group: connectivityGVK.Group, Resource: "connectivities"},
					obj.GetName(), errors.New("forbidden by test"))
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: forbidden, scheme: base.scheme}

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want the advertised pending capability state", err)
	}
	if !delegatedConnectivityExists(t, ctx, stack.Name) {
		t.Error("inaccessible delegated Connectivity must be left for the external operator/API to recover")
	}
	if gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Error("GatewayHTTPAPI must still be deleted independently")
	}
	if credentialsExist(t, ctx, stack.Name) {
		t.Error("god-mode Credentials must still be revoked independently")
	}
}

// A transient connectivity-operator outage with a healthy v3 ledger (gate still
// open) must NOT flap the already-provisioned resources.
func TestConnectivityReconcileKeepsResourcesWhenCapabilityUnavailableButLedgerV3(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = false
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = true

	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.Name = "stack0-connectivity"
	cred := newLedgerCredentialsForStack("stack0")

	ctx := newReconcileTestContext(t, ledger, httpAPI, cred)

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if !gatewayHTTPAPIExists(t, ctx, "stack0") {
		t.Error("GatewayHTTPAPI must be kept while the ledger gate is still open")
	}
	if !credentialsExist(t, ctx, "stack0") {
		t.Error("Credentials must be kept while the ledger gate is still open")
	}
}

// A failure to delete one resource during the hard teardown must not leave the
// others behind: the public GatewayHTTPAPI and the god-mode Credentials still
// have to be removed even when the delegated Connectivity delete fails (e.g. its
// CRD/API was removed after startup), and the failure must surface.
func TestTeardownDelegatedAttemptsEveryDeletion(t *testing.T) {
	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.Name = "stack0-connectivity"
	cred := newLedgerCredentialsForStack("stack0")

	base := newReconcileTestContext(t, httpAPI, cred)
	failing := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetObjectKind().GroupVersionKind() == connectivityGVK {
				return apierrors.NewInternalError(errors.New("delegated Connectivity delete failed"))
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: failing, scheme: base.scheme}

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	if err := teardownDelegated(ctx, stack, connectivity); err == nil {
		t.Fatal("teardownDelegated must surface the delegated Connectivity delete failure")
	}
	if gatewayHTTPAPIExists(t, ctx, "stack0") {
		t.Error("GatewayHTTPAPI must still be torn down when the delegated Connectivity delete fails")
	}
	if credentialsExist(t, ctx, "stack0") {
		t.Error("god-mode Credentials must still be torn down when the delegated Connectivity delete fails")
	}
}

func TestConnectivityReconcileRequeuesWhenLedgerCredentialsWatchUnavailable(t *testing.T) {
	previousConnectivityAvailable := connectivityAvailable
	previousWatchAvailable := ledgerCredentialsWatchAvailable
	connectivityAvailable = true
	ledgerCredentialsWatchAvailable = false
	t.Cleanup(func() {
		connectivityAvailable = previousConnectivityAvailable
		ledgerCredentialsWatchAvailable = previousWatchAvailable
	})

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = true
	credentials := newLedgerCredentialsForStack("stack0")
	ctx := newReconcileTestContext(t, ledger, credentials)

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if core.ApplicationErrorRequeueAfter(err) <= 0 {
		t.Fatalf("pending credentials without a watch must request a delayed requeue, got %v", err)
	}
}

func TestConnectivityReconcileUsesWatchWhenLedgerCredentialsArePending(t *testing.T) {
	previousConnectivityAvailable := connectivityAvailable
	previousWatchAvailable := ledgerCredentialsWatchAvailable
	connectivityAvailable = true
	ledgerCredentialsWatchAvailable = true
	t.Cleanup(func() {
		connectivityAvailable = previousConnectivityAvailable
		ledgerCredentialsWatchAvailable = previousWatchAvailable
	})

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = true
	credentials := newLedgerCredentialsForStack("stack0")
	ctx := newReconcileTestContext(t, ledger, credentials)

	stack := &v1beta1.Stack{}
	stack.Name = "stack0"
	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if delay := core.ApplicationErrorRequeueAfter(err); delay != 0 {
		t.Fatalf("ready-state watch should drive the next reconcile without polling, got delay %s", delay)
	}
}

func TestConnectivityReconcilePendingWhenLedgerCredentialsAPIUnavailable(t *testing.T) {
	previous := connectivityAvailable
	previousWatchAvailable := ledgerCredentialsWatchAvailable
	connectivityAvailable = true
	ledgerCredentialsWatchAvailable = true
	t.Cleanup(func() {
		connectivityAvailable = previous
		ledgerCredentialsWatchAvailable = previousWatchAvailable
	})

	cases := []struct {
		name string
		err  error
	}{
		{"missing CRD", &apimeta.NoKindMatchError{GroupKind: ledgerCredentialsGVK.GroupKind()}},
		{"absent RBAC", apierrors.NewForbidden(
			schema.GroupResource{Group: ledgerCredentialsGVK.Group, Resource: "credentials"},
			"connectivity-stack0", errors.New("forbidden by test"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := &v1beta1.Ledger{}
			ledger.Name = "stack0-ledger"
			ledger.Spec.Stack = "stack0"
			ledger.Spec.Version = "v3.0.0"
			ledger.Status.Ready = true

			base := newReconcileTestContext(t, ledger)
			failing := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if obj.GetObjectKind().GroupVersionKind() == ledgerCredentialsGVK {
						return tc.err
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})
			ctx := credsTestContext{Context: context.Background(), client: failing, scheme: base.scheme}

			stack := &v1beta1.Stack{}
			stack.Name = "stack0"
			connectivity := &v1beta1.Connectivity{}
			connectivity.Name = "stack0"
			connectivity.Spec.Stack = "stack0"

			err := Reconcile(ctx, stack, connectivity, "v1.0.0")
			if !core.IsApplicationError(err) {
				t.Fatalf("Reconcile() returned %v, want an application (pending) error when the ledger Credentials API is unavailable", err)
			}
			if core.ApplicationErrorRequeueAfter(err) <= 0 {
				t.Fatalf("unavailable Credentials API must be polled even when its watch was registered at startup, got %v", err)
			}
			if len(connectivity.Status.Conditions) == 0 || connectivity.Status.Conditions[0].Reason != "LedgerCredentialsUnavailable" {
				t.Fatalf("expected a LedgerCredentialsUnavailable condition, got %#v", connectivity.Status.Conditions)
			}
		})
	}
}

func TestConnectivityReconcileDialsLedgerServiceAndKeepsSNIForTLS(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = true

	credentials := newLedgerCredentialsForStack("stack0")
	_ = unstructured.SetNestedField(credentials.Object, "Ready", "status", "phase")
	_ = unstructured.SetNestedField(credentials.Object, "key-id", "status", "keyID")
	_ = unstructured.SetNestedSlice(credentials.Object, []any{
		map[string]any{"namespace": "stack0", "name": "connectivity-ledger-key"},
	}, "status", "distributedSecretRefs")

	ctx := newReconcileTestContext(t, ledger, credentials)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if err == nil || !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending while the delegated resource has no Ready status", err)
	}

	delegated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, delegated); err != nil {
		t.Fatalf("get delegated Connectivity: %v", err)
	}
	if address, _, _ := unstructured.NestedString(delegated.Object, "spec", "ledgerAddress"); address != "ledger-stack0:8888" {
		t.Fatalf("spec.ledgerAddress = %q, want the in-namespace Service endpoint ledger-stack0:8888", address)
	}
	if serverName, _, _ := unstructured.NestedString(delegated.Object, "spec", "ledgerTLS", "serverName"); serverName != "ledger-stack0.stack0.svc.cluster.local" {
		t.Fatalf("spec.ledgerTLS.serverName = %q, want the certificate SNI to remain unchanged", serverName)
	}
}

func TestConnectivityReconcileReturnsPendingAfterUpdatingReadyDelegatedSpec(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = true

	credentials := newLedgerCredentialsForStack("stack0")
	_ = unstructured.SetNestedField(credentials.Object, "Ready", "status", "phase")
	_ = unstructured.SetNestedField(credentials.Object, "key-id", "status", "keyID")
	_ = unstructured.SetNestedSlice(credentials.Object, []any{
		map[string]any{"namespace": "stack0", "name": "connectivity-ledger-key"},
	}, "status", "distributedSecretRefs")

	delegated := newDelegatedConnectivity("stack0")
	_ = unstructured.SetNestedField(delegated.Object, "old-ledger:9999", "spec", "ledgerAddress")
	_ = unstructured.SetNestedField(delegated.Object, "Ready", "status", "phase")

	ctx := newReconcileTestContext(t, ledger, credentials, delegated)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending after changing the delegated spec", err)
	}
	updated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, updated); err != nil {
		t.Fatalf("get updated delegated Connectivity: %v", err)
	}
	if address, _, _ := unstructured.NestedString(updated.Object, "spec", "ledgerAddress"); address != "ledger-stack0:8888" {
		t.Fatalf("updated spec.ledgerAddress = %q, want ledger-stack0:8888", address)
	}
}

// The connectivity-api Service is named after the (now fixed) delegated
// resource name, so the gateway backend must point at "connectivity-api".
func TestConnectivityAPIBackendRef(t *testing.T) {
	ref := connectivityAPIBackendRef()
	if ref.Name != "connectivity-api" {
		t.Fatalf("connectivityAPIBackendRef().Name = %q, want %q", ref.Name, "connectivity-api")
	}
	if ref.Port != connectivityAPIPort {
		t.Fatalf("connectivityAPIBackendRef().Port = %d, want %d", ref.Port, connectivityAPIPort)
	}
}
