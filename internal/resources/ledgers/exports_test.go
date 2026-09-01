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

// newHasV3Context builds a context whose fake client carries the Settings
// indexes the settings package lists with, so the v3 preview Setting lookup
// resolves.
func newHasV3Context(t *testing.T, objects ...client.Object) ledgerV3DiscoveryContext {
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

// TestHasV3 proves the connectivity-facing gate accepts both a v3 module
// version and a v2 ledger running the v3 preview, while mirroring the ledger
// reconciler's own decision to ignore the preview Setting when the Ledger
// Operator CRD is unavailable.
func TestHasV3(t *testing.T) {
	previous := ledgerV3ClusterAvailable
	t.Cleanup(func() { ledgerV3ClusterAvailable = previous })

	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}}
	previewSetting := settings.New("preview", "ledger.v3.preview-version", "v3.0.0-alpha.11", stack.Name)

	tests := []struct {
		name             string
		ledgerVersion    string
		clusterAvailable bool
		objects          []client.Object
		want             bool
		wantErr          bool
	}{
		{
			name:          "v3 module version needs no preview nor cluster capability",
			ledgerVersion: "v3.0.0",
			want:          true,
		},
		{
			name:             "v2 module version with the v3 preview enabled",
			ledgerVersion:    "v2.2.19",
			clusterAvailable: true,
			objects:          []client.Object{previewSetting},
			want:             true,
		},
		{
			name:             "v2 module version without a preview Setting",
			ledgerVersion:    "v2.2.19",
			clusterAvailable: true,
			want:             false,
		},
		{
			name:          "preview Setting is ignored when the Ledger Operator CRD is unavailable",
			ledgerVersion: "v2.2.19",
			objects:       []client.Object{previewSetting},
			want:          false,
		},
		{
			name:             "invalid preview Setting surfaces an error",
			ledgerVersion:    "v2.2.19",
			clusterAvailable: true,
			objects:          []client.Object{settings.New("preview", "ledger.v3.preview-version", "v2.5.0", stack.Name)},
			wantErr:          true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledgerV3ClusterAvailable = tt.clusterAvailable

			got, err := HasV3(newHasV3Context(t, tt.objects...), stack, tt.ledgerVersion)
			if tt.wantErr {
				if err == nil {
					t.Fatal("HasV3() must surface the invalid preview Setting")
				}
				return
			}
			if err != nil {
				t.Fatalf("HasV3() returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("HasV3() = %v, want %v", got, tt.want)
			}
		})
	}
}
