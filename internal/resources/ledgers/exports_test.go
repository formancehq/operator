package ledgers

import (
	"context"
	"strconv"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
// indexes the settings package lists with, so the v3 preview Setting lookup
// resolves.
func newPreviewContext(t *testing.T, objects ...client.Object) ledgerV3DiscoveryContext {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
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

// TestV3PreviewReady proves consumers can distinguish a reconciled, running
// preview from a Ledger whose status predates the preview Setting.
func TestV3PreviewReady(t *testing.T) {
	tests := []struct {
		name       string
		conditions v1beta1.Conditions
		want       bool
	}{
		{
			name: "no preview condition (stale v2-only status)",
			want: false,
		},
		{
			name:       "preview condition pending",
			conditions: v1beta1.Conditions{{Type: ledgerV3PreviewReadyCondition, Status: metav1.ConditionFalse}},
			want:       false,
		},
		{
			name:       "preview condition running",
			conditions: v1beta1.Conditions{{Type: ledgerV3PreviewReadyCondition, Status: metav1.ConditionTrue}},
			want:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledger := &v1beta1.Ledger{}
			ledger.Status.Conditions = tt.conditions
			if got := V3PreviewReady(ledger); got != tt.want {
				t.Fatalf("V3PreviewReady() = %v, want %v", got, tt.want)
			}
		})
	}
}
