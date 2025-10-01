package manifests

import (
	"context"
	"testing"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVersionMatching(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		versionRange string
		expected     bool
	}{
		// Exact match
		{
			name:         "exact match",
			version:      "v2.2.0",
			versionRange: "v2.2.0",
			expected:     true,
		},
		// Range tests
		{
			name:         "within range",
			version:      "v2.2.5",
			versionRange: ">=v2.2.0 <v2.3.0",
			expected:     true,
		},
		{
			name:         "below range",
			version:      "v2.1.0",
			versionRange: ">=v2.2.0 <v2.3.0",
			expected:     false,
		},
		{
			name:         "above range",
			version:      "v2.3.0",
			versionRange: ">=v2.2.0 <v2.3.0",
			expected:     false,
		},
		{
			name:         "at lower bound",
			version:      "v2.2.0",
			versionRange: ">=v2.2.0 <v2.3.0",
			expected:     true,
		},
		// Latest/invalid semver
		{
			name:         "latest version",
			version:      "latest",
			versionRange: ">=v2.3.0",
			expected:     true,
		},
	}

	loader := &Loader{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loader.versionMatches(tt.version, tt.versionRange)
			if got != tt.expected {
				t.Errorf("versionMatches(%q, %q) = %v, want %v",
					tt.version, tt.versionRange, got, tt.expected)
			}
		})
	}
}

func TestLoadLedgerManifests(t *testing.T) {
	ctx := context.Background()

	// Create test manifests
	manifests := []v1beta1.VersionManifest{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ledger-v2-0",
				Labels: map[string]string{
					"formance.com/component": "ledger",
				},
			},
			Spec: v1beta1.VersionManifestSpec{
				Component:    "ledger",
				VersionRange: ">=v2.0.0 <v2.2.0",
				Architecture: v1beta1.ArchitectureConfig{
					Type: "single-or-multi-writer",
					Deployments: []v1beta1.DeploymentSpec{
						{Name: "ledger"},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ledger-v2-2",
				Labels: map[string]string{
					"formance.com/component": "ledger",
				},
			},
			Spec: v1beta1.VersionManifestSpec{
				Component:    "ledger",
				VersionRange: ">=v2.2.0 <v2.3.0",
				Architecture: v1beta1.ArchitectureConfig{
					Type: "stateless",
					Deployments: []v1beta1.DeploymentSpec{
						{Name: "ledger"},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ledger-v2-3",
				Labels: map[string]string{
					"formance.com/component": "ledger",
				},
			},
			Spec: v1beta1.VersionManifestSpec{
				Component:    "ledger",
				VersionRange: ">=v2.3.0",
				Architecture: v1beta1.ArchitectureConfig{
					Type: "stateless",
					Deployments: []v1beta1.DeploymentSpec{
						{Name: "ledger"},
						{Name: "ledger-worker"},
					},
				},
			},
		},
	}

	// Create scheme and add types
	scheme := runtime.NewScheme()
	_ = v1beta1.AddToScheme(scheme)

	// Create fake client with manifests
	objs := make([]runtime.Object, len(manifests))
	for i := range manifests {
		objs[i] = &manifests[i]
	}
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()

	// Initialize loader
	loader := &Loader{client: client}

	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{
			name:    "load v2.0.5",
			version: "v2.0.5",
			wantErr: false,
		},
		{
			name:    "load v2.2.0",
			version: "v2.2.0",
			wantErr: false,
		},
		{
			name:    "load v2.3.1",
			version: "v2.3.1",
			wantErr: false,
		},
		{
			name:    "load latest",
			version: "latest",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := loader.Load(ctx, "ledger", tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if manifest == nil {
					t.Error("Load() returned nil manifest")
					return
				}
				if manifest.Spec.Component != "ledger" {
					t.Errorf("manifest.Spec.Component = %q, want 'ledger'", manifest.Spec.Component)
				}
				t.Logf("Loaded manifest for %s: architecture=%s, envPrefix=%q",
					tt.version, manifest.Spec.Architecture.Type, manifest.Spec.EnvVarPrefix)
			}
		})
	}
}

// TestManifestValidation is now handled by Kubernetes CRD validation
// The kubebuilder markers in versionmanifest_types.go enforce validation rules

func TestManifestInheritance(t *testing.T) {
	parent := &v1beta1.VersionManifest{
		Spec: v1beta1.VersionManifestSpec{
			EnvVarPrefix: "NUMARY_",
			Features: map[string]bool{
				"feature1": true,
				"feature2": false,
			},
			Streams: v1beta1.StreamsConfig{
				Ingestion: "streams/ledger",
				Reindex:   "assets/reindex/v1.0.0",
			},
		},
	}

	child := &v1beta1.VersionManifest{
		Spec: v1beta1.VersionManifestSpec{
			Extends: "parent",
			Features: map[string]bool{
				"feature2": true, // Override
				"feature3": true, // New
			},
			Streams: v1beta1.StreamsConfig{
				Reindex: "assets/reindex/v2.0.0", // Override
			},
		},
	}

	loader := &Loader{}
	merged := loader.mergeManifests(parent, child)

	// Check inherited values
	if merged.Spec.EnvVarPrefix != "" {
		t.Errorf("EnvVarPrefix not inherited, got %q", merged.Spec.EnvVarPrefix)
	}

	if merged.Spec.Streams.Ingestion != "streams/ledger" {
		t.Errorf("Streams.Ingestion not inherited, got %q", merged.Spec.Streams.Ingestion)
	}

	// Check overridden values
	if merged.Spec.Streams.Reindex != "assets/reindex/v2.0.0" {
		t.Errorf("Streams.Reindex not overridden, got %q", merged.Spec.Streams.Reindex)
	}

	// Check feature merging
	if !merged.Spec.Features["feature1"] {
		t.Error("feature1 not inherited")
	}

	if !merged.Spec.Features["feature2"] {
		t.Error("feature2 not overridden to true")
	}

	if !merged.Spec.Features["feature3"] {
		t.Error("feature3 not added")
	}
}
