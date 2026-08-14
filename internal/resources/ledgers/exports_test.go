package ledgers

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
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

func TestIsV3(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"v2.4.0":          false,
		"v3.0.0-alpha":    false,
		"v3.0.0-alpha.1":  true,
		"v3.0.0":          true,
		"v4.0.0-rc.1":     true,
		"invalid-version": false,
	}
	for version, expected := range tests {
		if actual := IsV3(version); actual != expected {
			t.Errorf("IsV3(%q) = %t, want %t", version, actual, expected)
		}
	}
}

func TestV3GRPCBackendRefPropagatesConfigurationLookupError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("configuration lookup failed")
	tests := map[string]int{
		"stack configuration lookup":    1,
		"wildcard configuration lookup": 2,
	}
	for name, failOnCall := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseClient := newExportsContext(t).client
			ctx := ledgerV3DiscoveryContext{
				Context: context.Background(),
				client: &failingLedgerV3Client{
					Client:     baseClient,
					err:        wantErr,
					failOnCall: failOnCall,
				},
			}
			_, err := V3GRPCBackendRef(ctx, "stack0")
			if !errors.Is(err, wantErr) {
				t.Fatalf("V3GRPCBackendRef() error = %v, want %v", err, wantErr)
			}
		})
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
