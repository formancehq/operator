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
