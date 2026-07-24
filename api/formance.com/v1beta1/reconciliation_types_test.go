package v1beta1

import "testing"

func TestReconciliationPublishesEvents(t *testing.T) {
	tests := map[string]bool{
		"v2.3.1":         false,
		"v3.0.0-0":       true,
		"v3.0.0-alpha.1": true,
		"v3.0.0-rc.1":    true,
		"v3.0.0":         true,
		"main":           true,
	}

	reconciliation := Reconciliation{}
	for version, expected := range tests {
		t.Run(version, func(t *testing.T) {
			if got := reconciliation.PublishesEvents(version); got != expected {
				t.Fatalf("PublishesEvents(%q) = %v, want %v", version, got, expected)
			}
		})
	}
}
