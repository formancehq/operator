package ledgers

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type failingLedgerV3DiscoveryReader struct {
	err error
}

func (r failingLedgerV3DiscoveryReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return r.err
}

func (r failingLedgerV3DiscoveryReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return r.err
}

type inaccessibleLedgerV3ResourceReader struct {
	err error
}

func (r inaccessibleLedgerV3ResourceReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return r.err
}

func (r inaccessibleLedgerV3ResourceReader) List(_ context.Context, object client.ObjectList, _ ...client.ListOption) error {
	switch list := object.(type) {
	case *apiextensionsv1.CustomResourceDefinitionList:
		list.Items = []apiextensionsv1.CustomResourceDefinition{{
			ObjectMeta: metav1.ObjectMeta{Name: "clusters.ledger.formance.com"},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "ledger.formance.com",
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "Cluster", Plural: "clusters"},
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
					Name: "v1alpha1", Served: true, Storage: true,
				}},
			},
		}}
		return nil
	case *unstructured.UnstructuredList:
		return r.err
	default:
		return nil
	}
}

type ledgerV3DiscoveryContext struct {
	context.Context
	reader client.Reader
	client client.Client
}

func (c ledgerV3DiscoveryContext) GetClient() client.Client    { return c.client }
func (c ledgerV3DiscoveryContext) GetScheme() *runtime.Scheme  { return nil }
func (c ledgerV3DiscoveryContext) GetAPIReader() client.Reader { return c.reader }
func (c ledgerV3DiscoveryContext) GetPlatform() core.Platform  { return core.Platform{} }

type ledgerV3AccessReviewClient struct {
	client.Client
	deniedVerb string
}

type ledgerV3CleanupClient struct {
	client.Client
	getCalls    int
	deleteCalls int
	secret      *corev1.Secret
}

func (c *ledgerV3CleanupClient) Get(_ context.Context, key client.ObjectKey, object client.Object, _ ...client.GetOption) error {
	c.getCalls++
	if secret, ok := object.(*corev1.Secret); ok && c.secret != nil {
		c.secret.DeepCopyInto(secret)
		return nil
	}
	return apierrors.NewNotFound(schema.GroupResource{Group: ledgerV3ClusterGVK.Group, Resource: "clusters"}, key.Name)
}

func (c *ledgerV3CleanupClient) Delete(_ context.Context, _ client.Object, _ ...client.DeleteOption) error {
	c.deleteCalls++
	return nil
}

func (c ledgerV3AccessReviewClient) Create(_ context.Context, object client.Object, _ ...client.CreateOption) error {
	review := object.(*authorizationv1.SelfSubjectAccessReview)
	review.Status.Allowed = review.Spec.ResourceAttributes.Verb != c.deniedVerb
	if !review.Status.Allowed {
		review.Status.Reason = "denied by test"
	}
	return nil
}

func TestLedgerV3DiscoveryFailureDisablesCapabilityWithoutFailing(t *testing.T) {
	previousClusterAvailable := ledgerV3ClusterAvailable
	previousCertManagerAvailable := ledgerV3CertManagerAvailable
	ledgerV3ClusterAvailable = true
	ledgerV3CertManagerAvailable = true
	t.Cleanup(func() {
		ledgerV3ClusterAvailable = previousClusterAvailable
		ledgerV3CertManagerAvailable = previousCertManagerAvailable
	})

	options := core.ReconcilerOptions[*v1beta1.Ledger]{}
	withLedgerV3ClusterWatch()(&options)
	if len(options.Raws) != 1 {
		t.Fatalf("withLedgerV3ClusterWatch() registered %d raw builders, want 1", len(options.Raws))
	}

	discoveryError := errors.New("CRD discovery forbidden")
	ctx := ledgerV3DiscoveryContext{
		Context: context.Background(),
		reader:  failingLedgerV3DiscoveryReader{err: discoveryError},
	}
	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("Ledger v3 discovery failure must not fail controller setup: %v", err)
	}
	if ledgerV3ClusterAvailable {
		t.Fatal("Ledger v3 Cluster capability remains enabled after discovery failure")
	}
	if ledgerV3CertManagerAvailable {
		t.Fatal("Ledger v3 cert-manager capability remains enabled after discovery failure")
	}
}

func validLedgerV3ClusterCRD() apiextensionsv1.CustomResourceDefinition {
	stringSchema := apiextensionsv1.JSONSchemaProps{Type: "string"}
	return apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: ledgerV3ClusterCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: ledgerV3ClusterGVK.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: ledgerV3ClusterGVK.Kind, Plural: "clusters"},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    ledgerV3ClusterGVK.Version,
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"sinks": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"nats": {
									Type: "array",
									Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{
										Type:     "object",
										Required: []string{"name", "topic", "url"},
										Properties: map[string]apiextensionsv1.JSONSchemaProps{
											"name": stringSchema, "topic": stringSchema, "url": stringSchema,
										},
									}},
								},
							}},
						}},
						"status": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"appliedSinks": {
								Type:  "array",
								Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &stringSchema},
							},
						}},
					},
				}},
			}},
		},
	}
}

func TestLedgerV3ClusterSupportsSinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*apiextensionsv1.CustomResourceDefinition)
		want   bool
	}{
		{name: "complete sink contract", want: true},
		{name: "version not served", mutate: func(crd *apiextensionsv1.CustomResourceDefinition) { crd.Spec.Versions[0].Served = false }},
		{name: "missing OpenAPI schema", mutate: func(crd *apiextensionsv1.CustomResourceDefinition) { crd.Spec.Versions[0].Schema.OpenAPIV3Schema = nil }},
		{name: "missing desired sink schema", mutate: func(crd *apiextensionsv1.CustomResourceDefinition) {
			delete(crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties, "sinks")
		}},
		{name: "wrong desired sink type", mutate: func(crd *apiextensionsv1.CustomResourceDefinition) {
			spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
			sinks := spec.Properties["sinks"]
			sinks.Type = "array"
			spec.Properties["sinks"] = sinks
		}},
		{name: "missing NATS sink schema", mutate: func(crd *apiextensionsv1.CustomResourceDefinition) {
			spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
			sinks := spec.Properties["sinks"]
			delete(sinks.Properties, "nats")
			spec.Properties["sinks"] = sinks
		}},
		{name: "missing required NATS field", mutate: func(crd *apiextensionsv1.CustomResourceDefinition) {
			spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
			sinks := spec.Properties["sinks"]
			nats := sinks.Properties["nats"]
			nats.Items.Schema.Required = []string{"name", "url"}
			sinks.Properties["nats"] = nats
			spec.Properties["sinks"] = sinks
		}},
		{name: "missing ownership status schema", mutate: func(crd *apiextensionsv1.CustomResourceDefinition) {
			delete(crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["status"].Properties, "appliedSinks")
		}},
		{name: "wrong ownership item type", mutate: func(crd *apiextensionsv1.CustomResourceDefinition) {
			status := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["status"]
			applied := status.Properties["appliedSinks"]
			applied.Items.Schema.Type = "integer"
			status.Properties["appliedSinks"] = applied
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			crd := validLedgerV3ClusterCRD()
			if test.mutate != nil {
				test.mutate(&crd)
			}
			crds := &apiextensionsv1.CustomResourceDefinitionList{Items: []apiextensionsv1.CustomResourceDefinition{crd}}
			if got := ledgerV3ClusterSupportsSinks(crds); got != test.want {
				t.Fatalf("ledgerV3ClusterSupportsSinks() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestLedgerV3ClusterSinkContractRefreshesAfterCRDUpgrade(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	crd := validLedgerV3ClusterCRD()
	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	delete(spec.Properties, "sinks")
	kubernetesClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&crd).Build()
	ctx := ledgerV3DiscoveryContext{Context: context.Background(), reader: kubernetesClient}

	supported, err := ledgerV3ClusterSupportsSinksAtRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if supported {
		t.Fatal("legacy CRD unexpectedly supports managed sinks")
	}

	updated := validLedgerV3ClusterCRD()
	updated.ResourceVersion = crd.ResourceVersion
	if err := kubernetesClient.Update(context.Background(), &updated); err != nil {
		t.Fatal(err)
	}
	supported, err = ledgerV3ClusterSupportsSinksAtRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !supported {
		t.Fatal("upgraded CRD sink contract was not discovered at reconciliation time")
	}
}

func TestLedgerV3ReconciliationRejectsIncompatibleSinkContract(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() { ledgerV3ClusterAvailable = previous })

	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	crd := validLedgerV3ClusterCRD()
	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	delete(spec.Properties, "sinks")
	kubernetesClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&crd).Build()
	ctx := ledgerV3DiscoveryContext{Context: context.Background(), reader: kubernetesClient}
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}

	tests := []struct {
		name          string
		conditionType string
		reconcile     func(*v1beta1.Ledger) error
	}{
		{
			name:          "primary Ledger v3",
			conditionType: ledgerV3ClusterReadyCondition,
			reconcile: func(ledger *v1beta1.Ledger) error {
				return reconcileV3(ctx, stack, ledger, "v3.0.0")
			},
		},
		{
			name:          "Ledger v3 preview",
			conditionType: ledgerV3PreviewReadyCondition,
			reconcile: func(ledger *v1beta1.Ledger) error {
				return reconcileV3Preview(ctx, stack, ledger, "v3.0.0")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ledger := &v1beta1.Ledger{ObjectMeta: metav1.ObjectMeta{Name: "ledger0", Generation: 3}}
			err := test.reconcile(ledger)
			if err == nil {
				t.Fatal("reconciliation unexpectedly accepted an incompatible Cluster CRD")
			}
			if got := core.ApplicationErrorRequeueAfter(err); got != ledgerV3CRDDiscoveryRetryDelay {
				t.Fatalf("requeue delay = %s, want %s", got, ledgerV3CRDDiscoveryRetryDelay)
			}
			condition := ledger.GetConditions().Get(test.conditionType)
			if condition == nil {
				t.Fatalf("missing %s condition", test.conditionType)
			}
			if condition.Status != metav1.ConditionFalse || condition.Reason != "OperatorIncompatible" {
				t.Fatalf("condition = status %s reason %q, want False/OperatorIncompatible", condition.Status, condition.Reason)
			}
		})
	}
}

func TestLedgerV3PreviewVersionIgnoredWhenClusterUnavailable(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = false
	t.Cleanup(func() {
		ledgerV3ClusterAvailable = previous
	})

	version, err := ledgerV3PreviewVersion(nil, nil)
	if err != nil {
		t.Fatalf("ledgerV3PreviewVersion() returned error: %v", err)
	}
	if version != "" {
		t.Fatalf("ledgerV3PreviewVersion() = %q, want an empty version", version)
	}
}

func TestDeleteLedgerV3PreviewRemovesSecretWhenCertManagerIsUnavailable(t *testing.T) {
	previous := ledgerV3CertManagerAvailable
	ledgerV3CertManagerAvailable = false
	t.Cleanup(func() {
		ledgerV3CertManagerAvailable = previous
	})

	kubernetesClient := &ledgerV3CleanupClient{secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: "stack0",
		Name:      ledgerV3TLSName("stack0"),
		Labels:    map[string]string{ledgerV3PreviewLabel: "true"},
	}}}
	ctx := ledgerV3DiscoveryContext{
		Context: context.Background(),
		client:  kubernetesClient,
	}
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	if err := deleteLedgerV3Preview(ctx, stack); err != nil {
		t.Fatalf("deleteLedgerV3Preview() returned error without cert-manager: %v", err)
	}
	if kubernetesClient.getCalls != 2 {
		t.Fatalf("deleteLedgerV3Preview() performed %d GETs, want Cluster and Secret lookups", kubernetesClient.getCalls)
	}
	if kubernetesClient.deleteCalls != 1 {
		t.Fatalf("deleteLedgerV3Preview() performed %d deletes, want the preview Secret deleted", kubernetesClient.deleteCalls)
	}
}

func TestLedgerV3ResourceAccessFailureDisablesCapabilityWithoutFailing(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() {
		ledgerV3ClusterAvailable = previous
	})

	options := core.ReconcilerOptions[*v1beta1.Ledger]{}
	withLedgerV3ClusterWatch()(&options)
	ctx := ledgerV3DiscoveryContext{
		Context: context.Background(),
		reader:  inaccessibleLedgerV3ResourceReader{err: errors.New("Cluster list forbidden")},
	}
	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("Ledger v3 resource access failure must not fail controller setup: %v", err)
	}
	if ledgerV3ClusterAvailable {
		t.Fatal("Ledger v3 Cluster capability remains enabled when Cluster objects are inaccessible")
	}
}

func TestLedgerV3MissingWatchPermissionDisablesCapabilityWithoutFailing(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	ledgerV3ClusterAvailable = true
	t.Cleanup(func() {
		ledgerV3ClusterAvailable = previous
	})

	options := core.ReconcilerOptions[*v1beta1.Ledger]{}
	withLedgerV3ClusterWatch()(&options)
	ctx := ledgerV3DiscoveryContext{
		Context: context.Background(),
		reader:  inaccessibleLedgerV3ResourceReader{},
		client:  ledgerV3AccessReviewClient{deniedVerb: "watch"},
	}
	if err := options.Raws[0](ctx, nil); err != nil {
		t.Fatalf("Ledger v3 partial RBAC must not fail controller setup: %v", err)
	}
	if ledgerV3ClusterAvailable {
		t.Fatal("Ledger v3 Cluster capability remains enabled without watch permission")
	}
}

func TestLegacyLedgerResourcesExistIncludesMigrationJobs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	ledger := &v1beta1.Ledger{ObjectMeta: metav1.ObjectMeta{Name: "ledger0"}}
	tests := []struct {
		name  string
		owner metav1.OwnerReference
	}{
		{
			name:  "Ledger migration Job",
			owner: metav1.OwnerReference{APIVersion: v1beta1.GroupVersion.String(), Kind: "Ledger", Name: ledger.Name},
		},
		{
			name:  "Database migration Job",
			owner: metav1.OwnerReference{APIVersion: v1beta1.GroupVersion.String(), Kind: "Database", Name: core.GetObjectName(stack.Name, "ledger")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
				Namespace:       stack.Name,
				Name:            "migration",
				OwnerReferences: []metav1.OwnerReference{tt.owner},
			}}
			kubernetesClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()
			ctx := ledgerV3DiscoveryContext{Context: context.Background(), client: kubernetesClient}

			exists, err := legacyLedgerResourcesExist(ctx, stack, ledger)
			if err != nil {
				t.Fatalf("legacyLedgerResourcesExist() returned error: %v", err)
			}
			if !exists {
				t.Fatal("legacyLedgerResourcesExist() ignored a legacy migration Job")
			}
		})
	}
}

func TestIsLedgerV3(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "legacy release", version: "v2.99.0", want: false},
		{name: "threshold", version: "v3.0.0-alpha", want: false},
		{name: "first alpha release", version: "v3.0.0-alpha.1", want: true},
		{name: "without v prefix", version: "3.0.0-alpha.1", want: true},
		{name: "stable v3", version: "v3.0.0", want: true},
		{name: "development tag remains legacy", version: "develop", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isLedgerV3(test.version); got != test.want {
				t.Fatalf("isLedgerV3(%q) = %t, want %t", test.version, got, test.want)
			}
		})
	}
}

func TestNormalizeLedgerV3Replicas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		configured     int32
		want           int32
		wantNormalized bool
		wantError      bool
	}{
		{name: "single replica", configured: 1, want: 1},
		{name: "odd replica count", configured: 3, want: 3},
		{name: "two replicas", configured: 2, want: 3, wantNormalized: true},
		{name: "four replicas", configured: 4, want: 5, wantNormalized: true},
		{name: "zero replicas", configured: 0, wantError: true},
		{name: "negative replicas", configured: -1, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, normalized, err := normalizeLedgerV3Replicas(test.configured)
			if test.wantError {
				if err == nil {
					t.Fatalf("normalizeLedgerV3Replicas(%d) expected an error", test.configured)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeLedgerV3Replicas(%d) returned error: %v", test.configured, err)
			}
			if got != test.want {
				t.Fatalf("normalizeLedgerV3Replicas(%d) = %d, want %d", test.configured, got, test.want)
			}
			if normalized != test.wantNormalized {
				t.Fatalf("normalizeLedgerV3Replicas(%d) normalized = %t, want %t", test.configured, normalized, test.wantNormalized)
			}
		})
	}
}

func TestIsV3ClusterReadyIncludesManagedSinks(t *testing.T) {
	t.Parallel()

	condition := func(status metav1.ConditionStatus, generation int64, reason, message string) *metav1.Condition {
		return &metav1.Condition{
			Type:               ledgerV3SinksSyncedCondition,
			Status:             status,
			ObservedGeneration: generation,
			Reason:             reason,
			Message:            message,
		}
	}
	tests := []struct {
		name          string
		managedSinks  bool
		condition     *metav1.Condition
		baseNotReady  bool
		wantReady     bool
		messageChecks []string
	}{
		{name: "unmanaged sinks preserve legacy readiness", wantReady: true},
		{name: "managed sinks wait for condition", managedSinks: true, messageChecks: []string{"sinksSynced=Unknown", "ConditionMissing"}},
		{name: "managed sinks propagate failure", managedSinks: true, condition: condition(metav1.ConditionFalse, 7, "Error", "name conflict"), messageChecks: []string{"sinksSynced=False", "sinksReason=Error", "name conflict"}},
		{name: "managed sinks reject stale success", managedSinks: true, condition: condition(metav1.ConditionTrue, 6, "Synced", "configured"), messageChecks: []string{"sinksSynced=True", "6/7"}},
		{name: "managed sinks preserve base readiness", managedSinks: true, condition: condition(metav1.ConditionTrue, 7, "Synced", "configured"), baseNotReady: true, messageChecks: []string{"phase=Pending", "sinksSynced=True"}},
		{name: "managed sinks accept current success", managedSinks: true, condition: condition(metav1.ConditionTrue, 7, "Synced", "configured"), wantReady: true, messageChecks: []string{"sinksSynced=True", "7/7"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cluster := newV3Cluster()
			cluster.SetGeneration(7)
			spec := map[string]interface{}{"replicas": int64(3)}
			if test.managedSinks {
				spec["sinks"] = map[string]interface{}{}
			}
			status := map[string]interface{}{
				"phase":              "Running",
				"readyReplicas":      int64(3),
				"observedGeneration": int64(7),
			}
			if test.baseNotReady {
				status["phase"] = "Pending"
			}
			if test.condition != nil {
				conditionMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(test.condition)
				if err != nil {
					t.Fatal(err)
				}
				status["conditions"] = []interface{}{conditionMap}
			}
			cluster.Object["spec"] = spec
			cluster.Object["status"] = status

			ready, message, err := isV3ClusterReady(cluster)
			if err != nil {
				t.Fatalf("isV3ClusterReady() returned error: %v", err)
			}
			if ready != test.wantReady {
				t.Fatalf("isV3ClusterReady() ready = %t, want %t; message=%q", ready, test.wantReady, message)
			}
			for _, check := range test.messageChecks {
				if !strings.Contains(message, check) {
					t.Fatalf("isV3ClusterReady() message %q does not contain %q", message, check)
				}
			}
		})
	}
}
