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
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
	if attrs, _, _ := unstructured.NestedString(object.Object, "spec", "monitoring", "attributes"); attrs != "pod-name=$(POD_NAME),stack=stack0" {
		t.Errorf("attributes = %q, want sorted key=value list", attrs)
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
