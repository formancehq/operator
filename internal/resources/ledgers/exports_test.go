package ledgers

import (
	"context"
	"strconv"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func newExportsContext(t *testing.T, objects ...client.Object) ledgerV3DiscoveryContext {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kubernetesClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&v1beta1.LedgerConfiguration{}, "stack", func(object client.Object) []string {
			return object.(*v1beta1.LedgerConfiguration).GetStacks()
		}).
		WithObjects(objects...).
		Build()
	return ledgerV3DiscoveryContext{Context: context.Background(), client: kubernetesClient}
}

func ledgerConfiguration(name string, grpcPort int32, stacks ...string) *v1beta1.LedgerConfiguration {
	return &v1beta1.LedgerConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1beta1.LedgerConfigurationSpec{
			Stacks: stacks,
			Cluster: ledgerv1alpha1.ClusterSpec{
				Service: ledgerv1alpha1.ServiceSpec{GrpcPort: grpcPort},
			},
		},
	}
}

// TestV3GRPCBackendRefHonoursConfiguredGrpcPort proves the connectivity-facing
// export resolves the same gRPC port as the gateway backend
// (LedgerConfiguration spec.cluster.service.grpcPort), rather than always
// defaulting to the standard port.
func TestV3GRPCBackendRefHonoursConfiguredGrpcPort(t *testing.T) {
	t.Parallel()

	const stackName = "stack0"
	tests := []struct {
		name     string
		objects  []client.Object
		wantPort int32
	}{
		{
			name:     "defaults to the standard gRPC port without a configuration",
			wantPort: ledgerV3GRPCPort,
		},
		{
			name:     "defaults to the standard gRPC port when grpcPort is unset",
			objects:  []client.Object{ledgerConfiguration("default", 0, stackName)},
			wantPort: ledgerV3GRPCPort,
		},
		{
			name:     "honours the stack LedgerConfiguration override",
			objects:  []client.Object{ledgerConfiguration("default", 7777, stackName)},
			wantPort: 7777,
		},
		{
			name:     "honours a wildcard LedgerConfiguration override",
			objects:  []client.Object{ledgerConfiguration("wildcard", 6666, "*")},
			wantPort: 6666,
		},
		{
			name: "stack-scoped configuration takes priority over the wildcard",
			objects: []client.Object{
				ledgerConfiguration("wildcard", 6666, "*"),
				ledgerConfiguration("scoped", 7777, stackName),
			},
			wantPort: 7777,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := newExportsContext(t, tt.objects...)

			backend, err := V3GRPCBackendRef(ctx, stackName)
			if err != nil {
				t.Fatalf("V3GRPCBackendRef() returned error: %v", err)
			}
			if backend.Port != tt.wantPort {
				t.Fatalf("V3GRPCBackendRef() port = %d, want %d", backend.Port, tt.wantPort)
			}
			if wantName := "ledger-" + stackName; backend.Name != wantName {
				t.Fatalf("V3GRPCBackendRef() name = %q, want %q", backend.Name, wantName)
			}
			if backend.TLS == nil {
				t.Fatal("V3GRPCBackendRef() must carry backend TLS material")
			}
			if wantServerName := "ledger-" + stackName + "." + stackName + ".svc.cluster.local"; backend.TLS.ServerName != wantServerName {
				t.Fatalf("V3GRPCBackendRef() server name = %q, want %q", backend.TLS.ServerName, wantServerName)
			}
		})
	}
}

// newPreviewContext builds a context whose fake client carries the Settings
// indexes the settings package lists with (so the v3 preview Setting lookup
// resolves) and knows the ledger Cluster GVK as unstructured (so the preview
// Cluster lookup resolves).
func newPreviewContext(t *testing.T, objects ...client.Object) ledgerV3DiscoveryContext {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	scheme.AddKnownTypeWithName(ledgerV3ClusterGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(ledgerV3ClusterGVK.GroupVersion().WithKind(ledgerV3ClusterGVK.Kind+"List"), &unstructured.UnstructuredList{})
	kubernetesClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&v1beta1.Settings{}, "stack", func(object client.Object) []string {
			return object.(*v1beta1.Settings).GetStacks()
		}).
		WithIndex(&v1beta1.Settings{}, "keylen", func(object client.Object) []string {
			return []string{strconv.Itoa(len(strings.Split(object.(*v1beta1.Settings).Spec.Key, ".")))}
		}).
		WithObjects(objects...).
		Build()
	return ledgerV3DiscoveryContext{Context: context.Background(), client: kubernetesClient}
}

// TestV3PreviewActive proves the connectivity-facing preview lookup mirrors
// the ledger reconciler's own decision, including ignoring the Setting when
// the Ledger Operator CRD is unavailable.
func TestV3PreviewActive(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	t.Cleanup(func() { ledgerV3ClusterAvailable = previous })

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	previewSetting := settings.New("preview", "ledger.v3.preview-version", "v3.0.0-alpha.11", stack.Name)

	tests := []struct {
		name             string
		clusterAvailable bool
		objects          []client.Object
		want             bool
		wantErr          bool
	}{
		{
			name:             "preview Setting enables the preview",
			clusterAvailable: true,
			objects:          []client.Object{previewSetting},
			want:             true,
		},
		{
			name:             "no preview Setting",
			clusterAvailable: true,
			want:             false,
		},
		{
			name:    "preview Setting is ignored when the Ledger Operator CRD is unavailable",
			objects: []client.Object{previewSetting},
			want:    false,
		},
		{
			name:             "invalid preview Setting surfaces an error",
			clusterAvailable: true,
			objects:          []client.Object{settings.New("preview", "ledger.v3.preview-version", "v2.5.0", stack.Name)},
			wantErr:          true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledgerV3ClusterAvailable = tt.clusterAvailable

			got, err := V3PreviewActive(newPreviewContext(t, tt.objects...), stack)
			if tt.wantErr {
				if err == nil {
					t.Fatal("V3PreviewActive() must surface the invalid preview Setting")
				}
				return
			}
			if err != nil {
				t.Fatalf("V3PreviewActive() returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("V3PreviewActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

// previewClusterOption mutates the canonical running preview Cluster used by
// TestV3PreviewReady to build its negative cases.
type previewClusterOption func(*unstructured.Unstructured)

// newRunningPreviewCluster returns the preview Cluster as createOrUpdateV3Cluster
// provisions it once the ledger operator has brought it up: preview label,
// preview-version annotation, and a Running status observed at the current
// generation.
func newRunningPreviewCluster(stackName, version string, options ...previewClusterOption) *unstructured.Unstructured {
	cluster := newV3Cluster()
	cluster.SetNamespace(stackName)
	cluster.SetName(stackName)
	cluster.SetGeneration(2)
	cluster.SetLabels(map[string]string{ledgerV3PreviewLabel: "true"})
	cluster.SetAnnotations(map[string]string{ledgerV3PreviewVersionAnnotation: version})
	_ = unstructured.SetNestedField(cluster.Object, int64(3), "spec", "replicas")
	_ = unstructured.SetNestedField(cluster.Object, "Running", "status", "phase")
	_ = unstructured.SetNestedField(cluster.Object, int64(3), "status", "readyReplicas")
	_ = unstructured.SetNestedField(cluster.Object, int64(2), "status", "observedGeneration")
	for _, option := range options {
		option(cluster)
	}
	return cluster
}

// TestV3PreviewReady proves the ready signal identifies the preview Cluster of
// the currently configured preview version, so status surviving a Setting
// remove/re-add (or predating the Setting entirely) never lets provisioning
// through against a deleting, not-yet-recreated, or older-version preview.
func TestV3PreviewReady(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	t.Cleanup(func() { ledgerV3ClusterAvailable = previous })

	const version = "v3.0.0-alpha.11"
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	previewSetting := settings.New("preview", "ledger.v3.preview-version", version, stack.Name)

	tests := []struct {
		name             string
		clusterAvailable bool
		objects          []client.Object
		want             bool
		wantErr          bool
	}{
		{
			name:             "running preview cluster for the configured version",
			clusterAvailable: true,
			objects:          []client.Object{previewSetting, newRunningPreviewCluster("stack0", version)},
			want:             true,
		},
		{
			name:             "no preview Setting",
			clusterAvailable: true,
			objects:          []client.Object{newRunningPreviewCluster("stack0", version)},
			want:             false,
		},
		{
			name:             "no cluster yet (ledger reconciler has not observed the Setting)",
			clusterAvailable: true,
			objects:          []client.Object{previewSetting},
			want:             false,
		},
		{
			name:             "cluster is not a preview (module-version v3 cluster)",
			clusterAvailable: true,
			objects: []client.Object{previewSetting, newRunningPreviewCluster("stack0", version, func(cluster *unstructured.Unstructured) {
				cluster.SetLabels(nil)
			})},
			want: false,
		},
		{
			name:             "cluster is being deleted (Setting removed then re-added)",
			clusterAvailable: true,
			objects: []client.Object{previewSetting, newRunningPreviewCluster("stack0", version, func(cluster *unstructured.Unstructured) {
				now := metav1.Now()
				cluster.SetDeletionTimestamp(&now)
				cluster.SetFinalizers([]string{"test/keep"})
			})},
			want: false,
		},
		{
			name:             "cluster still carries a previous preview version",
			clusterAvailable: true,
			objects: []client.Object{previewSetting, newRunningPreviewCluster("stack0", version, func(cluster *unstructured.Unstructured) {
				cluster.SetAnnotations(map[string]string{ledgerV3PreviewVersionAnnotation: "v3.0.0-alpha.10"})
			})},
			want: false,
		},
		{
			name:             "cluster spec not yet observed by the ledger operator",
			clusterAvailable: true,
			objects: []client.Object{previewSetting, newRunningPreviewCluster("stack0", version, func(cluster *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(cluster.Object, int64(1), "status", "observedGeneration")
			})},
			want: false,
		},
		{
			name:    "preview Setting ignored when the Ledger Operator CRD is unavailable",
			objects: []client.Object{previewSetting, newRunningPreviewCluster("stack0", version)},
			want:    false,
		},
		{
			name:             "invalid preview Setting surfaces an error",
			clusterAvailable: true,
			objects:          []client.Object{settings.New("preview", "ledger.v3.preview-version", "v2.5.0", stack.Name)},
			wantErr:          true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledgerV3ClusterAvailable = tt.clusterAvailable

			got, err := V3PreviewReady(newPreviewContext(t, tt.objects...), stack)
			if tt.wantErr {
				if err == nil {
					t.Fatal("V3PreviewReady() must surface the invalid preview Setting")
				}
				return
			}
			if err != nil {
				t.Fatalf("V3PreviewReady() returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("V3PreviewReady() = %v, want %v", got, tt.want)
			}
		})
	}
}
