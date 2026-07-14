package ledgers

import "testing"

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
