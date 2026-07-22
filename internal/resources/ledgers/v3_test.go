package ledgers

import (
	"context"
	"errors"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
