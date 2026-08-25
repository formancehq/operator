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

	appsv1 "k8s.io/api/apps/v1"
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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/auths"
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
	client    client.Client
	apiReader client.Reader
	scheme    *runtime.Scheme
}

func (c credsTestContext) GetClient() client.Client   { return c.client }
func (c credsTestContext) GetScheme() *runtime.Scheme { return c.scheme }
func (c credsTestContext) GetAPIReader() client.Reader {
	if c.apiReader != nil {
		return c.apiReader
	}
	return c.client
}
func (c credsTestContext) GetPlatform() core.Platform { return core.Platform{} }

type connectivityWatchTestManager struct {
	ctrl.Manager
	client client.Client
	scheme *runtime.Scheme
}

func (m connectivityWatchTestManager) GetClient() client.Client    { return m.client }
func (m connectivityWatchTestManager) GetAPIReader() client.Reader { return m.client }
func (m connectivityWatchTestManager) GetScheme() *runtime.Scheme  { return m.scheme }
func (m connectivityWatchTestManager) GetPlatform() core.Platform  { return core.Platform{} }

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

	connectivity := &v1beta1.Connectivity{}
	connectivity.Name = "stack0"
	connectivity.Spec.Stack = "stack0"
	now := metav1.Now()
	connectivity.DeletionTimestamp = &now
	connectivity.Finalizers = []string{"delete-ledger-credentials"}
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	setStackOwnedLedgerCredentials(stack, connectivity, existing)
	ctx := newCredsTestContext(t, existing)

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

func TestDeleteLedgerCredentialsFinalizerPreservesOwnerlessCredential(t *testing.T) {
	existing := newLedgerCredentialsForStack("stack0")
	existing.SetUID(types.UID("ownerless-credentials-uid"))
	ctx := newCredsTestContext(t, existing)
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = "stack0"

	if err := deleteLedgerCredentials(ctx, connectivity); err != nil {
		t.Fatalf("deleteLedgerCredentials() returned %v", err)
	}
	if !credentialsExist(t, ctx, "stack0") {
		t.Fatal("ownerless Credentials was deleted by fixed name")
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
	controller := core.ForModule(func(_ core.Context, _ *v1beta1.Stack, _ *core.ReconcilerOptions[*v1beta1.Connectivity], _ *v1beta1.Connectivity, _ string) error {
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

func TestAuthCreateEventEnqueuesMatchingConnectivity(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add v1beta1 to scheme: %v", err)
	}

	connectivity := newConnectivity("stack0-connectivity", "stack0")
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&v1beta1.Connectivity{}, "stack", unstructuredStackIndex).
		WithObjects(connectivity).
		Build()
	manager := connectivityWatchTestManager{client: fakeClient, scheme: scheme}

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{
		Owns:     map[client.Object][]builder.OwnsOption{},
		Watchers: map[client.Object]core.ReconcilerOptionsWatch{},
	}
	for _, option := range connectivityReconcilerOptions() {
		option(&options)
	}

	var authWatch core.ReconcilerOptionsWatch
	found := false
	for watched, watch := range options.Watchers {
		if _, ok := watched.(*v1beta1.Auth); ok {
			authWatch = watch
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Connectivity controller has no Auth event watch")
	}

	handler, _ := authWatch.Handler(manager, nil, &v1beta1.Connectivity{})
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()
	handler.Create(context.Background(), event.CreateEvent{Object: &v1beta1.Auth{
		ObjectMeta: metav1.ObjectMeta{Name: "stack0-auth"},
		Spec:       v1beta1.AuthSpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
	}}, queue)

	if queue.Len() != 1 {
		t.Fatalf("Auth create event queued %d reconciles, want 1", queue.Len())
	}
	request, _ := queue.Get()
	defer queue.Done(request)
	if request.Name != connectivity.Name {
		t.Fatalf("Auth create event queued Connectivity %q, want %q", request.Name, connectivity.Name)
	}
}

func TestGatewayCreateEventEnqueuesMatchingConnectivity(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add v1beta1 to scheme: %v", err)
	}

	connectivity := newConnectivity("stack0-connectivity", "stack0")
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&v1beta1.Connectivity{}, "stack", unstructuredStackIndex).
		WithObjects(connectivity).
		Build()
	manager := connectivityWatchTestManager{client: fakeClient, scheme: scheme}

	options := core.ReconcilerOptions[*v1beta1.Connectivity]{
		Owns:     map[client.Object][]builder.OwnsOption{},
		Watchers: map[client.Object]core.ReconcilerOptionsWatch{},
	}
	for _, option := range connectivityReconcilerOptions() {
		option(&options)
	}

	var gatewayWatch core.ReconcilerOptionsWatch
	found := false
	for watched, watch := range options.Watchers {
		if _, ok := watched.(*v1beta1.Gateway); ok {
			gatewayWatch = watch
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Connectivity controller has no Gateway event watch")
	}

	handler, _ := gatewayWatch.Handler(manager, nil, &v1beta1.Connectivity{})
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()
	handler.Create(context.Background(), event.CreateEvent{Object: &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "stack0-gateway"},
		Spec:       v1beta1.GatewaySpec{StackDependency: v1beta1.StackDependency{Stack: "stack0"}},
	}}, queue)

	if queue.Len() != 1 {
		t.Fatalf("Gateway create event queued %d reconciles, want 1", queue.Len())
	}
	request, _ := queue.Get()
	defer queue.Done(request)
	if request.Name != connectivity.Name {
		t.Fatalf("Gateway create event queued Connectivity %q, want %q", request.Name, connectivity.Name)
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

func (noMatchDeleteClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return &apimeta.NoKindMatchError{GroupKind: ledgerCredentialsGVK.GroupKind()}
}

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
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatalf("add apps v1 to scheme: %v", err)
	}
	s.AddKnownTypeWithName(connectivityGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(connectivityGVK.GroupVersion().WithKind(connectivityGVK.Kind+"List"), &unstructured.UnstructuredList{})
	s.AddKnownTypeWithName(ledgerCredentialsGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(ledgerCredentialsGVK.GroupVersion().WithKind(ledgerCredentialsGVK.Kind+"List"), &unstructured.UnstructuredList{})
	return credsTestContext{
		Context: context.Background(),
		scheme:  s,
		client: fake.NewClientBuilder().WithScheme(s).
			WithIndex(&v1beta1.Ledger{}, "stack", func(obj client.Object) []string {
				return []string{obj.(*v1beta1.Ledger).Spec.Stack}
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
			WithIndex(&v1beta1.Auth{}, "stack", unstructuredStackIndex).
			WithIndex(&v1beta1.Gateway{}, "stack", unstructuredStackIndex).
			WithObjects(objs...).Build(),
	}
}

// unstructuredStackIndex indexes module resources looked up through
// core.GetAllStackDependencies, which lists with an UnstructuredList: the fake
// client hands the unstructured form of the stored object to the index
// function, not the typed one.
func unstructuredStackIndex(object client.Object) []string {
	if value, found, err := unstructured.NestedString(object.(*unstructured.Unstructured).Object, "spec", "stack"); err == nil && found {
		return []string{value}
	}
	return nil
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

func connectivityAPITestLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":           "connectivity-api",
		"app.kubernetes.io/instance":       "connectivity-api",
		"connectivity.formance.com/parent": connectivityDelegatedName,
	}
}

func makeConnectivityAPIDeploymentRoutable(deployment *appsv1.Deployment) {
	deployment.Spec.Template.Labels = connectivityAPITestLabels()
	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name == "api" {
			deployment.Spec.Template.Spec.Containers[i].Ports = []corev1.ContainerPort{{
				Name:          "http",
				ContainerPort: connectivityAPIPort,
				Protocol:      corev1.ProtocolTCP,
			}}
			return
		}
	}
}

func newConnectivityAPIService(delegated *unstructured.Unstructured) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: delegated.GetNamespace(),
			Name:      connectivityDelegatedName + "-api",
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(delegated, connectivityGVK),
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: connectivityAPITestLabels(),
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       connectivityAPIPort,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromString("http"),
			}},
		},
	}
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

func setStackOwnedLedgerCredentials(
	stack *v1beta1.Stack,
	connectivity *v1beta1.Connectivity,
	credentials *unstructured.Unstructured,
) {
	// ForModule uses SetOwnerReference rather than SetControllerReference for a
	// module's Stack owner. Mirror that production shape here: only the
	// cluster-scoped Credentials has the Stack as its controller owner.
	connectivity.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: v1beta1.GroupVersion.String(),
		Kind:       "Stack",
		Name:       stack.Name,
		UID:        stack.UID,
	}})
	credentials.SetUID(types.UID(credentials.GetName() + "-uid"))
	credentials.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(stack, v1beta1.GroupVersion.WithKind("Stack")),
	})
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

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name

	// Pre-provisioned delegated resources, as if connectivity had been running
	// before the ledger was downgraded.
	delegated := newDelegatedConnectivity(stack.Name)
	delegated.SetUID(types.UID("delegated-connectivity-uid"))
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	cred := newLedgerCredentialsForStack(stack.Name)
	setStackOwnedLedgerCredentials(stack, connectivity, cred)

	ctx := newReconcileTestContext(t, ledger, delegated, httpAPI, cred)

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

func TestConnectivityReconcilePreservesForeignFixedNameResourcesOnHardGate(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v2.0.0"
	ledger.Status.Ready = true

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name
	foreignConnectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "foreign",
		UID:  types.UID("foreign-connectivity-uid"),
	}}
	foreignStack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{
		Name: "foreign",
		UID:  types.UID("foreign-stack-uid"),
	}}

	delegated := newDelegatedConnectivity(stack.Name)
	delegated.SetUID(types.UID("foreign-delegated-uid"))
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(foreignConnectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("foreign-gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(foreignConnectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	credentials := newLedgerCredentialsForStack(stack.Name)
	credentials.SetUID(types.UID("foreign-credentials-uid"))
	credentials.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(foreignStack, v1beta1.GroupVersion.WithKind("Stack")),
	})
	ctx := newReconcileTestContext(t, ledger, delegated, httpAPI, credentials)

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); err == nil {
		t.Fatal("Reconcile() returned nil on a non-v3 hard gate")
	}
	if !delegatedConnectivityExists(t, ctx, stack.Name) {
		t.Error("foreign-owned delegated Connectivity was deleted by name")
	}
	if !gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Error("foreign-owned GatewayHTTPAPI was deleted by name")
	}
	if !credentialsExist(t, ctx, stack.Name) {
		t.Error("foreign-owned ledger Credentials was deleted by name")
	}
}

func TestConnectivityReconcileTearsDownDelegatedWhenLedgerNotV3AndAPIAuthResolutionFails(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v2.0.0"
	ledger.Status.Ready = true

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name
	delegated := newDelegatedConnectivity(stack.Name)
	delegated.SetUID(types.UID("delegated-connectivity-uid"))
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	cred := newLedgerCredentialsForStack(stack.Name)
	setStackOwnedLedgerCredentials(stack, connectivity, cred)

	base := newReconcileTestContext(t, ledger, delegated, httpAPI, cred)
	failing := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if strings.HasPrefix(list.GetObjectKind().GroupVersionKind().Kind, "Auth") {
				return errors.New("auth list forbidden by test")
			}
			return c.List(ctx, list, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: failing, scheme: base.scheme}

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending for a resolved non-v3 Ledger", err)
	}
	if condition := connectivity.GetConditions().Get(connectivityReadyCondition); condition == nil || condition.Reason != "LedgerNotV3" {
		t.Fatalf("condition = %+v, want reason LedgerNotV3", condition)
	}
	if delegatedConnectivityExists(t, ctx, stack.Name) {
		t.Error("delegated Connectivity must be torn down despite an unrelated auth resolution failure")
	}
	if gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Error("GatewayHTTPAPI must be torn down despite an unrelated auth resolution failure")
	}
	if credentialsExist(t, ctx, stack.Name) {
		t.Error("god-mode Credentials must be torn down despite an unrelated auth resolution failure")
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

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name

	delegated := newDelegatedConnectivity(stack.Name)
	delegated.SetUID(types.UID("delegated-connectivity-uid"))
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	replicas := int32(1)
	apiDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  stack.Name,
			Name:       "connectivity-api",
			Generation: 1,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(delegated, connectivityGVK),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "api",
				Env:  []corev1.EnvVar{{Name: "AUTH_ENABLED", Value: "false"}},
			}}}},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
	makeConnectivityAPIDeploymentRoutable(apiDeployment)
	apiService := newConnectivityAPIService(delegated)
	cred := newLedgerCredentialsForStack("stack0")

	ctx := newReconcileTestContext(t, ledger, delegated, httpAPI, apiDeployment, apiService, cred)

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

func TestConnectivityReconcileUpdatesAPIAuthWhenLedgerV3NotReady(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = false

	delegated := newDelegatedConnectivity("stack0")
	_ = unstructured.SetNestedField(delegated.Object, true, "spec", "api", "enabled")

	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = "stack0"

	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = "stack0"
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}

	ctx := newReconcileTestContext(t, ledger, delegated, auth, gateway)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending while the ledger is not ready", err)
	}
	if condition := connectivity.GetConditions().Get(connectivityReadyCondition); condition == nil || condition.Reason != "LedgerNotReady" {
		t.Fatalf("condition = %+v, want reason LedgerNotReady", condition)
	}

	updated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, updated); err != nil {
		t.Fatalf("get updated delegated Connectivity: %v", err)
	}
	if issuer, _, _ := unstructured.NestedString(updated.Object, "spec", "api", "auth", "issuer"); issuer != "https://stack0.example.com/api/auth" {
		t.Fatalf("spec.api.auth.issuer = %q, want the stack auth issuer while the ledger is not ready", issuer)
	}
	if checkScopes, found, _ := unstructured.NestedBool(updated.Object, "spec", "api", "auth", "checkScopes"); !found || checkScopes {
		t.Fatalf("spec.api.auth.checkScopes = %v (found=%v), want an explicit false", checkScopes, found)
	}
}

func TestConnectivityReconcileRevokesGatewayWhileUpdatingAPIAuthWhenLedgerV3NotReady(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = false

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name

	delegated := newDelegatedConnectivity(stack.Name)
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	_ = unstructured.SetNestedField(delegated.Object, true, "spec", "api", "enabled")

	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = stack.Name
	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = stack.Name
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}

	ctx := newReconcileTestContext(t, ledger, delegated, httpAPI, auth, gateway)

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending while the ledger is not ready", err)
	}
	if gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Fatal("GatewayHTTPAPI remains exposed while auth is updated behind a transient ledger gate")
	}
	if !delegatedConnectivityExists(t, ctx, stack.Name) {
		t.Fatal("delegated Connectivity was torn down on a transient ledger gate")
	}
}

func TestConnectivityReconcileSynchronizesAuthAndRevokesGatewayBeforeCredentialsReady(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = true

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name

	delegated := newDelegatedConnectivity(stack.Name)
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	_ = unstructured.SetNestedField(delegated.Object, true, "spec", "api", "enabled")

	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = stack.Name
	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = stack.Name
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}
	credentials := newLedgerCredentialsForStack(stack.Name)
	_ = unstructured.SetNestedField(credentials.Object, "Pending", "status", "phase")

	ctx := newReconcileTestContext(t, ledger, delegated, httpAPI, auth, gateway, credentials)

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending while credentials or the auth rollout is pending", err)
	}
	updated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(updated), updated); err != nil {
		t.Fatalf("get delegated Connectivity: %v", err)
	}
	if issuer, _, _ := unstructured.NestedString(updated.Object, "spec", "api", "auth", "issuer"); issuer != "https://stack0.example.com/api/auth" {
		t.Fatalf("spec.api.auth.issuer = %q, want auth synchronized before the credential gate", issuer)
	}
	if gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Fatal("GatewayHTTPAPI remains exposed while auth changes before the credential gate")
	}
}

func TestConnectivityReconcileRevokesGatewayWhenAuthRolloutCannotBeProvenBehindLedgerNotReady(t *testing.T) {
	for _, tc := range []struct {
		name       string
		deployment bool
	}{
		{name: "Deployment absent"},
		{name: "Deployment still unauthenticated", deployment: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := connectivityAvailable
			connectivityAvailable = true
			t.Cleanup(func() { connectivityAvailable = previous })

			ledger := &v1beta1.Ledger{}
			ledger.Name = "stack0-ledger"
			ledger.Spec.Stack = "stack0"
			ledger.Spec.Version = "v3.0.0"
			ledger.Status.Ready = false

			stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
			connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
				Name: "stack0",
				UID:  types.UID("connectivity-uid"),
			}}
			connectivity.Spec.Stack = stack.Name

			delegated := newDelegatedConnectivity(stack.Name)
			delegated.SetUID(types.UID("delegated-connectivity-uid"))
			delegated.SetOwnerReferences([]metav1.OwnerReference{
				*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
			})
			_ = unstructured.SetNestedField(delegated.Object, "https://stack0.example.com/api/auth", "spec", "api", "auth", "issuer")
			_ = unstructured.SetNestedField(delegated.Object, false, "spec", "api", "auth", "checkScopes")

			httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
				Name: "stack0-connectivity",
				UID:  types.UID("gateway-http-api-uid"),
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
				},
			}}
			auth := &v1beta1.Auth{}
			auth.Name = "stack0-auth"
			auth.Spec.Stack = stack.Name
			gateway := &v1beta1.Gateway{}
			gateway.Name = "stack0-gateway"
			gateway.Spec.Stack = stack.Name
			gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}

			objects := []client.Object{ledger, delegated, httpAPI, auth, gateway}
			if tc.deployment {
				replicas := int32(1)
				objects = append(objects, &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:  stack.Name,
						Name:       "connectivity-api",
						Generation: 1,
						OwnerReferences: []metav1.OwnerReference{
							*metav1.NewControllerRef(delegated, connectivityGVK),
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: &replicas,
						Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
							Name: "api",
							Env:  []corev1.EnvVar{{Name: "AUTH_ENABLED", Value: "false"}},
						}}}},
					},
					Status: appsv1.DeploymentStatus{
						ObservedGeneration: 1,
						Replicas:           1,
						UpdatedReplicas:    1,
						ReadyReplicas:      1,
						AvailableReplicas:  1,
					},
				})
			}
			ctx := newReconcileTestContext(t, objects...)

			if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
				t.Fatalf("Reconcile() returned %v, want pending while the ledger is not ready", err)
			}
			if condition := connectivity.GetConditions().Get(connectivityReadyCondition); condition == nil || condition.Reason != "LedgerNotReady" {
				t.Fatalf("condition = %+v, want reason LedgerNotReady", condition)
			}
			if gatewayHTTPAPIExists(t, ctx, stack.Name) {
				t.Fatal("GatewayHTTPAPI remains exposed without a proven authenticated API rollout")
			}
		})
	}
}

func TestConnectivityAPIAuthRolloutRejectsForeignOwnedDelegatedConnectivity(t *testing.T) {
	expectedConnectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	expectedConnectivity.Spec.Stack = "stack0"
	foreignConnectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "foreign",
		UID:  types.UID("foreign-connectivity-uid"),
	}}

	delegated := newDelegatedConnectivity("stack0")
	delegated.SetUID(types.UID("delegated-connectivity-uid"))
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(foreignConnectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	_ = unstructured.SetNestedField(delegated.Object, "https://stack0.example.com/api/auth", "spec", "api", "auth", "issuer")
	_ = unstructured.SetNestedField(delegated.Object, false, "spec", "api", "auth", "checkScopes")

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "stack0",
			Name:       "connectivity-api",
			Generation: 1,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(delegated, connectivityGVK),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "api",
				Env: []corev1.EnvVar{
					{Name: "AUTH_ENABLED", Value: "true"},
					{Name: "AUTH_ISSUER", Value: "https://stack0.example.com/api/auth"},
					{Name: "AUTH_CHECK_SCOPES", Value: "false"},
				},
			}}}},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
	ctx := newReconcileTestContext(t, delegated, deployment)

	ready, _, err := connectivityAPIAuthRolloutReady(ctx, "stack0", expectedConnectivity, &auths.ProtectedAuthConfiguration{
		Issuer: "https://stack0.example.com/api/auth",
	})
	if ready {
		t.Fatal("foreign-owned delegated Connectivity allowed gateway reopening")
	}
	if err == nil {
		t.Fatalf("foreign ownership was not reported for expected Connectivity UID %q", expectedConnectivity.UID)
	}
}

func TestConnectivityAPIAuthRolloutRejectsForeignOwnedService(t *testing.T) {
	expectedConnectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	expectedConnectivity.Spec.Stack = "stack0"

	delegated := newDelegatedConnectivity("stack0")
	delegated.SetUID(types.UID("delegated-connectivity-uid"))
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(expectedConnectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	_ = unstructured.SetNestedField(delegated.Object, "https://stack0.example.com/api/auth", "spec", "api", "auth", "issuer")
	_ = unstructured.SetNestedField(delegated.Object, false, "spec", "api", "auth", "checkScopes")

	replicas := int32(1)
	podLabels := map[string]string{"app.kubernetes.io/name": "connectivity-api"}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "stack0",
			Name:       "connectivity-api",
			Generation: 1,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(delegated, connectivityGVK),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "api",
					Env: []corev1.EnvVar{
						{Name: "AUTH_ENABLED", Value: "true"},
						{Name: "AUTH_ISSUER", Value: "https://stack0.example.com/api/auth"},
						{Name: "AUTH_CHECK_SCOPES", Value: "false"},
					},
				}}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
	foreignDelegated := newDelegatedConnectivity("stack0")
	foreignDelegated.SetName("foreign")
	foreignDelegated.SetUID(types.UID("foreign-delegated-connectivity-uid"))
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Namespace: "stack0",
		Name:      "connectivity-api",
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(foreignDelegated, connectivityGVK),
		},
	}, Spec: corev1.ServiceSpec{
		Selector: podLabels,
		Ports:    []corev1.ServicePort{{Port: connectivityAPIPort}},
	}}
	ctx := newReconcileTestContext(t, delegated, deployment, service)

	ready, _, err := connectivityAPIAuthRolloutReady(ctx, "stack0", expectedConnectivity, &auths.ProtectedAuthConfiguration{
		Issuer: "https://stack0.example.com/api/auth",
	})
	if ready {
		t.Fatal("foreign-owned connectivity API Service allowed gateway reopening")
	}
	if err == nil {
		t.Fatal("foreign Service ownership was not reported")
	}
}

func TestConnectivityAPIServiceRoutesOnlyToExpectedDeployment(t *testing.T) {
	delegated := newDelegatedConnectivity("stack0")
	deployment := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}}},
	}}
	makeConnectivityAPIDeploymentRoutable(deployment)
	service := newConnectivityAPIService(delegated)
	if !connectivityAPIServiceRoutesToDeployment(service, deployment) {
		t.Fatal("official connectivity API Service shape was rejected")
	}

	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Service)
	}{
		{
			name: "empty selector",
			mutate: func(service *corev1.Service) {
				service.Spec.Selector = nil
			},
		},
		{
			name: "selector misses API pods",
			mutate: func(service *corev1.Service) {
				service.Spec.Selector["app.kubernetes.io/name"] = "foreign-api"
			},
		},
		{
			name: "gateway backend port missing",
			mutate: func(service *corev1.Service) {
				service.Spec.Ports[0].Port = 9090
			},
		},
		{
			name: "target port misses API container",
			mutate: func(service *corev1.Service) {
				service.Spec.Ports[0].TargetPort = intstr.FromString("foreign-http")
			},
		},
		{
			name: "external name bypasses selector",
			mutate: func(service *corev1.Service) {
				service.Spec.Type = corev1.ServiceTypeExternalName
				service.Spec.ExternalName = "unauthenticated.example.com"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unsafeService := service.DeepCopy()
			tc.mutate(unsafeService)
			if connectivityAPIServiceRoutesToDeployment(unsafeService, deployment) {
				t.Fatal("unsafe connectivity API Service was accepted")
			}
		})
	}
}

func TestReconcileExistingConnectivityAPIAuthRevokesGatewayBeforePatch(t *testing.T) {
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = "stack0"
	delegated := newDelegatedConnectivity(connectivity.Spec.Stack)
	delegated.SetUID(types.UID("delegated-connectivity-uid"))
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}

	base := newReconcileTestContext(t, delegated, httpAPI)
	operations := make([]string, 0, 2)
	orderedClient := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.DeleteOption) error {
			if _, ok := object.(*v1beta1.GatewayHTTPAPI); ok {
				operations = append(operations, "delete gateway")
			}
			return c.Delete(ctx, object, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, object client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if object.GetObjectKind().GroupVersionKind() == connectivityGVK {
				operations = append(operations, "patch delegated")
			}
			return c.Patch(ctx, object, patch, opts...)
		},
	})
	ctx := credsTestContext{
		Context:   context.Background(),
		client:    orderedClient,
		apiReader: base.client,
		scheme:    base.scheme,
	}

	changed, err := reconcileExistingConnectivityAPIAuth(ctx, connectivity.Spec.Stack, connectivity,
		&auths.ProtectedAuthConfiguration{Issuer: "https://stack0.example.com/api/auth"})
	if err != nil {
		t.Fatalf("reconcileExistingConnectivityAPIAuth() returned %v", err)
	}
	if !changed {
		t.Fatal("reconcileExistingConnectivityAPIAuth() did not report the auth change")
	}
	want := []string{"delete gateway", "patch delegated"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %v, want %v", operations, want)
	}
}

func TestConnectivityReconcileAdoptsOwnerlessDelegatedWhenAPIAuthAlreadyMatches(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = false

	delegated := newDelegatedConnectivity("stack0")
	_ = unstructured.SetNestedField(delegated.Object, "https://stack0.example.com/api/auth", "spec", "api", "auth", "issuer")
	_ = unstructured.SetNestedField(delegated.Object, false, "spec", "api", "auth", "checkScopes")

	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = "stack0"
	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = "stack0"
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}

	ctx := newReconcileTestContext(t, ledger, delegated, auth, gateway)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending while the ledger is not ready", err)
	}

	updated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, updated); err != nil {
		t.Fatalf("get adopted delegated Connectivity: %v", err)
	}
	owner := metav1.GetControllerOf(updated)
	if owner == nil || owner.UID != connectivity.UID {
		t.Fatalf("controller owner = %+v, want Connectivity UID %q", owner, connectivity.UID)
	}
}

func TestUpdateExistingConnectivityAPIAuthRejectsSameNameReplacement(t *testing.T) {
	controller := true
	delegated := newDelegatedConnectivity("stack0")
	delegated.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: v1beta1.GroupVersion.String(),
		Kind:       "Connectivity",
		Name:       "stack0",
		UID:        types.UID("connectivity-uid"),
		Controller: &controller,
	}})
	_ = unstructured.SetNestedField(delegated.Object, "https://old.example.com/api/auth", "spec", "api", "auth", "issuer")

	base := newReconcileTestContext(t, delegated)
	replaced := false
	replacingClient := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, object client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if !replaced && object.GetObjectKind().GroupVersionKind() == connectivityGVK {
				replaced = true
				current := newDelegatedConnectivity("stack0")
				if err := c.Get(ctx, client.ObjectKeyFromObject(current), current); err != nil {
					return err
				}
				if err := c.Delete(ctx, current); err != nil {
					return err
				}

				replacement := newDelegatedConnectivity("stack0")
				replacement.SetOwnerReferences([]metav1.OwnerReference{{
					APIVersion: v1beta1.GroupVersion.String(),
					Kind:       "Connectivity",
					Name:       "foreign-connectivity",
					UID:        types.UID("foreign-connectivity-uid"),
					Controller: &controller,
				}})
				_ = unstructured.SetNestedField(replacement.Object, "https://foreign.example.com/api/auth", "spec", "api", "auth", "issuer")
				if err := c.Create(ctx, replacement); err != nil {
					return err
				}
			}
			return c.Patch(ctx, object, patch, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: replacingClient, scheme: base.scheme}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}

	_, err := updateExistingConnectivityAPIAuth(ctx, "stack0", connectivity, &auths.ProtectedAuthConfiguration{
		Issuer: "https://stack0.example.com/api/auth",
	})
	if !apierrors.IsConflict(err) {
		t.Fatalf("updateExistingConnectivityAPIAuth() returned %v, want conflict after same-name replacement", err)
	}

	replacement := newDelegatedConnectivity("stack0")
	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(replacement), replacement); err != nil {
		t.Fatalf("get same-name replacement: %v", err)
	}
	if issuer, _, _ := unstructured.NestedString(replacement.Object, "spec", "api", "auth", "issuer"); issuer != "https://foreign.example.com/api/auth" {
		t.Fatalf("replacement auth issuer = %q, want foreign issuer unchanged", issuer)
	}
}

func TestConnectivityReconcileAvoidsCachedReadAfterEarlyAPIAuthWrite(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger, credentials := newReadyLedgerPrerequisites()
	base := newReconcileTestContext(t, ledger, credentials)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	if err := Reconcile(base, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("initial Reconcile() returned %v, want pending after creating the delegated resource", err)
	}
	stale := newDelegatedConnectivity(stack.Name)
	if err := base.GetClient().Get(base, client.ObjectKeyFromObject(stale), stale); err != nil {
		t.Fatalf("get delegated Connectivity: %v", err)
	}
	_ = unstructured.SetNestedField(stale.Object, "Ready", "status", "phase")
	if err := base.GetClient().Update(base, stale); err != nil {
		t.Fatalf("mark delegated Connectivity ready: %v", err)
	}
	if err := base.GetClient().Get(base, client.ObjectKeyFromObject(stale), stale); err != nil {
		t.Fatalf("refresh delegated Connectivity before auth change: %v", err)
	}

	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = stack.Name
	if err := base.GetClient().Create(base, auth); err != nil {
		t.Fatalf("create Auth: %v", err)
	}
	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = stack.Name
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}
	if err := base.GetClient().Create(base, gateway); err != nil {
		t.Fatalf("create Gateway: %v", err)
	}

	patched := false
	servedStaleRead := false
	staleCacheClient := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
			if patched && !servedStaleRead && object.GetObjectKind().GroupVersionKind() == connectivityGVK && key == client.ObjectKeyFromObject(stale) {
				servedStaleRead = true
				stale.DeepCopyInto(object.(*unstructured.Unstructured))
				return nil
			}
			return c.Get(ctx, key, object, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, object client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if object.GetObjectKind().GroupVersionKind() == connectivityGVK {
				if err := c.Patch(ctx, object, patch, opts...); err != nil {
					return err
				}
				patched = true
				return nil
			}
			return c.Patch(ctx, object, patch, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: staleCacheClient, scheme: base.scheme}

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending after applying the auth change once", err)
	}
	if condition := connectivity.GetConditions().Get(connectivityReadyCondition); condition == nil || condition.Reason != "ConnectivityAPIPending" {
		t.Fatalf("condition = %+v, want ConnectivityAPIPending rather than a cached-write conflict", condition)
	}
}

func TestConnectivityReconcileDoesNotPatchAPIAuthOnForeignOwnedDelegatedResource(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = false

	delegated := newDelegatedConnectivity("stack0")
	_ = unstructured.SetNestedField(delegated.Object, "https://foreign.example.com/api/auth", "spec", "api", "auth", "issuer")
	controller := true
	foreignOwners := []metav1.OwnerReference{{
		APIVersion: v1beta1.GroupVersion.String(),
		Kind:       "Connectivity",
		Name:       "foreign-connectivity",
		UID:        types.UID("foreign-connectivity-uid"),
		Controller: &controller,
	}}
	delegated.SetOwnerReferences(foreignOwners)

	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = "stack0"
	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = "stack0"
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}

	ctx := newReconcileTestContext(t, ledger, delegated, auth, gateway)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if err == nil || core.IsApplicationError(err) {
		t.Errorf("Reconcile() returned %v, want a hard ownership error", err)
	}
	if condition := connectivity.GetConditions().Get(connectivityReadyCondition); condition == nil || condition.Reason != "APIAuthReconcileFailed" {
		t.Errorf("condition = %+v, want reason APIAuthReconcileFailed", condition)
	}

	updated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, updated); err != nil {
		t.Fatalf("get foreign-owned delegated Connectivity: %v", err)
	}
	if issuer, _, _ := unstructured.NestedString(updated.Object, "spec", "api", "auth", "issuer"); issuer != "https://foreign.example.com/api/auth" {
		t.Errorf("foreign-owned spec.api.auth.issuer = %q, want it unchanged", issuer)
	}
	if !reflect.DeepEqual(updated.GetOwnerReferences(), foreignOwners) {
		t.Errorf("foreign-owned ownerReferences = %#v, want %#v", updated.GetOwnerReferences(), foreignOwners)
	}
}

func TestConnectivityReconcileUpdatesAPIAuthWhenLedgerVersionUnresolved(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Status.Ready = true

	delegated := newDelegatedConnectivity("stack0")
	_ = unstructured.SetNestedField(delegated.Object, true, "spec", "api", "enabled")

	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = "stack0"

	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = "stack0"
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}

	ctx := newReconcileTestContext(t, ledger, delegated, auth, gateway)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	stack.Spec.VersionsFromFile = "v3.0.0"
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending while the Ledger version is unresolved", err)
	}
	if condition := connectivity.GetConditions().Get(connectivityReadyCondition); condition == nil || condition.Reason != "LedgerVersionUnresolved" {
		t.Fatalf("condition = %+v, want reason LedgerVersionUnresolved", condition)
	}

	updated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, updated); err != nil {
		t.Fatalf("get updated delegated Connectivity: %v", err)
	}
	if issuer, _, _ := unstructured.NestedString(updated.Object, "spec", "api", "auth", "issuer"); issuer != "https://stack0.example.com/api/auth" {
		t.Fatalf("spec.api.auth.issuer = %q, want the stack auth issuer while the Ledger version is unresolved", issuer)
	}
}

func TestConnectivityReconcilePropagatesUnexpectedLedgerVersionError(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Status.Ready = true

	base := newReconcileTestContext(t, ledger)
	versionErr := errors.New("versions lookup unavailable")
	failing := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*v1beta1.Versions); ok {
				return versionErr
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: failing, scheme: base.scheme}

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	stack.Spec.VersionsFromFile = "v3.0.0"
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if !errors.Is(err, versionErr) {
		t.Fatalf("Reconcile() returned %v, want the unexpected version lookup error", err)
	}
	if core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned an application error for an unexpected version lookup failure: %v", err)
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
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	cred := newLedgerCredentialsForStack(stack.Name)
	setStackOwnedLedgerCredentials(stack, connectivity, cred)

	ctx := newReconcileTestContext(t, httpAPI, cred)

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

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name
	delegated := newDelegatedConnectivity(stack.Name)
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	cred := newLedgerCredentialsForStack(stack.Name)
	setStackOwnedLedgerCredentials(stack, connectivity, cred)
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

// When the connectivity capability cannot be inspected, keep the workload and
// its ledger Credentials but close the public route: the authenticated rollout
// cannot be proven while discovery/RBAC is unavailable.
func TestConnectivityReconcileRevokesGatewayWhenCapabilityUnavailableButLedgerV3(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = false
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger := &v1beta1.Ledger{}
	ledger.Name = "stack0-ledger"
	ledger.Spec.Stack = "stack0"
	ledger.Spec.Version = "v3.0.0"
	ledger.Status.Ready = true

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("stack-uid"),
	}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	cred := newLedgerCredentialsForStack("stack0")

	ctx := newReconcileTestContext(t, ledger, httpAPI, cred)

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want an application (pending) error", err)
	}
	if gatewayHTTPAPIExists(t, ctx, "stack0") {
		t.Error("GatewayHTTPAPI remains exposed while the authenticated rollout cannot be proven")
	}
	if !credentialsExist(t, ctx, "stack0") {
		t.Error("Credentials must be kept while the ledger gate is still open")
	}
}

func TestConnectivityReconcileRevokesGatewayWhenLedgerLookupFails(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("stack-uid"),
	}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}

	base := newReconcileTestContext(t, httpAPI)
	failing := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*v1beta1.LedgerList); ok {
				return errors.New("ledger list forbidden by test")
			}
			return c.List(ctx, list, opts...)
		},
	})
	ctx := credsTestContext{
		Context:   context.Background(),
		client:    failing,
		apiReader: base.client,
		scheme:    base.scheme,
	}

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); err == nil || core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want the ledger lookup error", err)
	}
	if gatewayHTTPAPIExists(t, base, stack.Name) {
		t.Fatal("GatewayHTTPAPI remains exposed after the ledger lookup became unverifiable")
	}
}

// A failure to delete one resource during the hard teardown must not leave the
// others behind: the public GatewayHTTPAPI and the god-mode Credentials still
// have to be removed even when the delegated Connectivity delete fails (e.g. its
// CRD/API was removed after startup), and the failure must surface.
func TestTeardownDelegatedAttemptsEveryDeletion(t *testing.T) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = stack.Name
	delegated := newDelegatedConnectivity(stack.Name)
	delegated.SetUID(types.UID("delegated-connectivity-uid"))
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	cred := newLedgerCredentialsForStack(stack.Name)
	setStackOwnedLedgerCredentials(stack, connectivity, cred)

	base := newReconcileTestContext(t, delegated, httpAPI, cred)
	failing := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetObjectKind().GroupVersionKind() == connectivityGVK {
				return apierrors.NewInternalError(errors.New("delegated Connectivity delete failed"))
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: failing, scheme: base.scheme}

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
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name
	delegated.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
	})

	ctx := newReconcileTestContext(t, ledger, credentials, delegated)

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

// newReadyLedgerPrerequisites returns a ready v3 Ledger and its provisioned
// god-mode Credentials for stack0, the common prerequisites of a Reconcile
// reaching the delegated-resource provisioning.
func newReadyLedgerPrerequisites() (*v1beta1.Ledger, *unstructured.Unstructured) {
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
	return ledger, credentials
}

func TestConnectivityReconcileWiresAPIAuthWhenStackHasAuth(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger, credentials := newReadyLedgerPrerequisites()

	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = "stack0"

	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = "stack0"
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}

	ctx := newReconcileTestContext(t, ledger, credentials, auth, gateway)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending while the delegated resource has no Ready status", err)
	}

	delegated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, delegated); err != nil {
		t.Fatalf("get delegated Connectivity: %v", err)
	}
	if issuer, _, _ := unstructured.NestedString(delegated.Object, "spec", "api", "auth", "issuer"); issuer != "https://stack0.example.com/api/auth" {
		t.Fatalf("spec.api.auth.issuer = %q, want the stack auth issuer https://stack0.example.com/api/auth", issuer)
	}
	checkScopes, found, _ := unstructured.NestedBool(delegated.Object, "spec", "api", "auth", "checkScopes")
	if !found || checkScopes {
		t.Fatalf("spec.api.auth.checkScopes = %v (found=%v), want an explicit false without a check-scopes Setting", checkScopes, found)
	}
}

func TestConnectivityReconcileAPIAuthHonorsCheckScopesSetting(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger, credentials := newReadyLedgerPrerequisites()

	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = "stack0"

	checkScopesSetting := settings.New("check-scopes", "auth.connectivity.check-scopes", "true", "stack0")

	ctx := newReconcileTestContext(t, ledger, credentials, auth, checkScopesSetting)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending while the delegated resource has no Ready status", err)
	}

	delegated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, delegated); err != nil {
		t.Fatalf("get delegated Connectivity: %v", err)
	}
	if checkScopes, _, _ := unstructured.NestedBool(delegated.Object, "spec", "api", "auth", "checkScopes"); !checkScopes {
		t.Fatal("spec.api.auth.checkScopes = false, want true when the auth.connectivity.check-scopes Setting is enabled")
	}
}

func TestConnectivityReconcileFailsWhenAPIAuthResolutionFails(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger, credentials := newReadyLedgerPrerequisites()

	base := newReconcileTestContext(t, ledger, credentials)
	failing := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if strings.HasPrefix(list.GetObjectKind().GroupVersionKind().Kind, "Auth") {
				return errors.New("auth list forbidden by test")
			}
			return c.List(ctx, list, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: failing, scheme: base.scheme}
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if err == nil || core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want a hard error when the stack Auth module cannot be resolved", err)
	}
	condition := connectivity.GetConditions().Get(connectivityReadyCondition)
	if condition == nil || condition.Reason != "APIAuthResolveFailed" {
		t.Fatalf("condition = %+v, want reason APIAuthResolveFailed", condition)
	}
}

func TestConnectivityReconcileRevokesOwnedGatewayWhenAPIAuthResolutionFails(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger, credentials := newReadyLedgerPrerequisites()
	controller := true
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       "Connectivity",
			Name:       "stack0",
			UID:        types.UID("connectivity-uid"),
			Controller: &controller,
		}},
	}}

	base := newReconcileTestContext(t, ledger, credentials, httpAPI)
	failing := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if strings.HasPrefix(list.GetObjectKind().GroupVersionKind().Kind, "Auth") {
				return errors.New("auth list forbidden by test")
			}
			return c.List(ctx, list, opts...)
		},
	})
	ctx := credsTestContext{Context: context.Background(), client: failing, scheme: base.scheme}
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	err := Reconcile(ctx, stack, connectivity, "v1.0.0")
	if err == nil || core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want a hard auth-resolution error", err)
	}
	if gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Fatal("owned GatewayHTTPAPI remains exposed after auth resolution failed")
	}
}

func TestRevokeGatewayHTTPAPIUsesDirectReadAndUIDPrecondition(t *testing.T) {
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0",
		UID:  types.UID("connectivity-uid"),
	}}
	connectivity.Spec.Stack = "stack0"
	httpAPI := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity",
		UID:  types.UID("gateway-http-api-uid"),
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity")),
		},
	}}
	base := newReconcileTestContext(t, httpAPI)
	deleteCalled := false
	staleCache := interceptor.NewClient(base.client.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
			if _, ok := object.(*v1beta1.GatewayHTTPAPI); ok {
				return apierrors.NewNotFound(schema.GroupResource{Group: v1beta1.GroupVersion.Group, Resource: "gatewayhttpapis"}, key.Name)
			}
			return c.Get(ctx, key, object, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.DeleteOption) error {
			if _, ok := object.(*v1beta1.GatewayHTTPAPI); ok {
				deleteCalled = true
				options := (&client.DeleteOptions{}).ApplyOptions(opts)
				if options.Preconditions == nil || options.Preconditions.UID == nil ||
					*options.Preconditions.UID != httpAPI.UID {
					t.Fatalf("delete UID precondition = %+v, want %q", options.Preconditions, httpAPI.UID)
				}
			}
			return c.Delete(ctx, object, opts...)
		},
	})
	ctx := credsTestContext{
		Context:   context.Background(),
		client:    staleCache,
		apiReader: base.client,
		scheme:    base.scheme,
	}

	if err := revokeGatewayHTTPAPI(ctx, connectivity); err != nil {
		t.Fatalf("revokeGatewayHTTPAPI() returned %v", err)
	}
	if !deleteCalled {
		t.Fatal("revokeGatewayHTTPAPI trusted a stale cached NotFound instead of the direct reader")
	}
	if gatewayHTTPAPIExists(t, base, connectivity.Spec.Stack) {
		t.Fatal("GatewayHTTPAPI still exists after direct-read revocation")
	}
}

func TestConnectivityReconcileKeepsGatewayClosedUntilAPIAuthRollout(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger, credentials := newReadyLedgerPrerequisites()
	ctx := newReconcileTestContext(t, ledger, credentials)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("initial Reconcile() returned %v, want pending after creating the delegated resource", err)
	}
	delegated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(delegated), delegated); err != nil {
		t.Fatalf("get delegated Connectivity: %v", err)
	}
	delegated.SetUID(types.UID("delegated-connectivity-uid"))
	_ = unstructured.SetNestedField(delegated.Object, "Ready", "status", "phase")
	if err := ctx.GetClient().Update(ctx, delegated); err != nil {
		t.Fatalf("mark delegated Connectivity ready: %v", err)
	}

	controller := true
	replicas := int32(1)
	apiDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  stack.Name,
			Name:       "connectivity-api",
			UID:        types.UID("connectivity-api-deployment-uid"),
			Generation: 1,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: connectivityGVK.GroupVersion().String(),
				Kind:       connectivityGVK.Kind,
				Name:       connectivityDelegatedName,
				UID:        delegated.GetUID(),
				Controller: &controller,
			}},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "api",
				Env:  []corev1.EnvVar{{Name: "AUTH_ENABLED", Value: "false"}},
			}}}},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
	makeConnectivityAPIDeploymentRoutable(apiDeployment)
	if err := ctx.GetClient().Create(ctx, apiDeployment); err != nil {
		t.Fatalf("create ready unauthenticated API Deployment: %v", err)
	}
	if err := ctx.GetClient().Create(ctx, newConnectivityAPIService(delegated)); err != nil {
		t.Fatalf("create connectivity API Service: %v", err)
	}
	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); err != nil {
		t.Fatalf("Reconcile() with a converged unauthenticated API returned %v", err)
	}

	httpAPI := &v1beta1.GatewayHTTPAPI{}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: stack.Name + "-connectivity"}, httpAPI); err != nil {
		t.Fatalf("get initial GatewayHTTPAPI: %v", err)
	}
	httpAPI.SetUID(types.UID("gateway-http-api-uid"))
	if err := ctx.GetClient().Update(ctx, httpAPI); err != nil {
		t.Fatalf("assign GatewayHTTPAPI test UID: %v", err)
	}

	auth := &v1beta1.Auth{}
	auth.Name = "stack0-auth"
	auth.Spec.Stack = stack.Name
	if err := ctx.GetClient().Create(ctx, auth); err != nil {
		t.Fatalf("create Auth: %v", err)
	}
	gateway := &v1beta1.Gateway{}
	gateway.Name = "stack0-gateway"
	gateway.Spec.Stack = stack.Name
	gateway.Spec.Ingress = &v1beta1.GatewayIngress{Scheme: "https", Host: "stack0.example.com"}
	if err := ctx.GetClient().Create(ctx, gateway); err != nil {
		t.Fatalf("create Gateway: %v", err)
	}

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending after enabling API auth", err)
	}
	if gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Fatal("GatewayHTTPAPI remains exposed while API auth is changing")
	}

	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(apiDeployment), apiDeployment); err != nil {
		t.Fatalf("get API Deployment before rollout: %v", err)
	}
	apiDeployment.Generation = 2
	apiDeployment.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
		{Name: "AUTH_ENABLED", Value: "true"},
		{Name: "AUTH_ISSUER", Value: "https://stack0.example.com/api/auth"},
		{Name: "AUTH_CHECK_SCOPES", Value: "false"},
	}
	if err := ctx.GetClient().Update(ctx, apiDeployment); err != nil {
		t.Fatalf("apply authenticated API Deployment template: %v", err)
	}
	apiDeployment.Status = appsv1.DeploymentStatus{
		ObservedGeneration: 1,
		Replicas:           2,
		UpdatedReplicas:    1,
		ReadyReplicas:      1,
		AvailableReplicas:  1,
	}
	if err := ctx.GetClient().Status().Update(ctx, apiDeployment); err != nil {
		t.Fatalf("start authenticated API rollout: %v", err)
	}

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending during API auth rollout", err)
	}
	if gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Fatal("GatewayHTTPAPI was reopened before the authenticated rollout completed")
	}

	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(apiDeployment), apiDeployment); err != nil {
		t.Fatalf("get API Deployment to complete rollout: %v", err)
	}
	apiDeployment.Status = appsv1.DeploymentStatus{
		ObservedGeneration: apiDeployment.Generation,
		Replicas:           1,
		UpdatedReplicas:    1,
		ReadyReplicas:      1,
		AvailableReplicas:  1,
	}
	if err := ctx.GetClient().Status().Update(ctx, apiDeployment); err != nil {
		t.Fatalf("complete authenticated API rollout: %v", err)
	}

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); err != nil {
		t.Fatalf("Reconcile() after authenticated rollout returned %v", err)
	}
	if !gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Fatal("GatewayHTTPAPI was not restored after the authenticated rollout completed")
	}
}

func TestConnectivityReconcileClearsAPIAuthWhenStackHasNoAuth(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })

	ledger, credentials := newReadyLedgerPrerequisites()

	delegated := newDelegatedConnectivity("stack0")
	_ = unstructured.SetNestedField(delegated.Object, "https://stale.example.com/api/auth", "spec", "api", "auth", "issuer")
	_ = unstructured.SetNestedField(delegated.Object, "Ready", "status", "phase")

	ctx := newReconcileTestContext(t, ledger, credentials, delegated)
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name

	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile() returned %v, want pending after changing the delegated spec", err)
	}

	updated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityDelegatedName}, updated); err != nil {
		t.Fatalf("get updated delegated Connectivity: %v", err)
	}
	if _, found, _ := unstructured.NestedMap(updated.Object, "spec", "api", "auth"); found {
		t.Fatal("spec.api.auth still present, want it cleared when the stack has no Auth module")
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
