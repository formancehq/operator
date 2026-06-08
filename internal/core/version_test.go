package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateMinimumVersion(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		wantError bool
	}{
		// Canonical semver below minimum is rejected.
		{name: "v2.0.0 rejected", version: "v2.0.0", wantError: true},
		{name: "v2.1.0 rejected", version: "v2.1.0", wantError: true},
		{name: "v2.1.9 rejected", version: "v2.1.9", wantError: true},
		{name: "v2.0.0-rc.5 rejected", version: "v2.0.0-rc.5", wantError: true},
		{name: "v2.2.0-alpha pre-release rejected", version: "v2.2.0-alpha", wantError: true},

		// Canonical semver at or above minimum is accepted.
		{name: "v2.2.0 accepted", version: "v2.2.0", wantError: false},
		{name: "v2.3.0 accepted", version: "v2.3.0", wantError: false},
		{name: "v3.0.0 accepted", version: "v3.0.0", wantError: false},

		// Partial semver (`v3`, `v3.2`) is expanded to its canonical form
		// (`v3.0.0`, `v3.2.0`) and gated by the same minimum-version check.
		{name: "v3 accepted (expands to v3.0.0)", version: "v3", wantError: false},
		{name: "v3.2 accepted (expands to v3.2.0)", version: "v3.2", wantError: false},
		{name: "v2.2 accepted as equal to minimum (expands to v2.2.0)", version: "v2.2", wantError: false},
		{name: "v2 rejected (expands to v2.0.0)", version: "v2", wantError: true},
		{name: "v2.1 rejected (expands to v2.1.0)", version: "v2.1", wantError: true},

		// Non-semver names (dev tags, SHA refs, non-`v`-prefixed strings)
		// pass through unchecked.
		{name: "non-semver accepted", version: "main", wantError: false},
		{name: "sha ref accepted", version: "abc123def", wantError: false},
		{name: "latest accepted", version: "latest", wantError: false},
		{name: "non-v-prefixed accepted as passthrough", version: "2.2.0", wantError: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMinimumVersion(tt.version)
			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "not supported")
				assert.Contains(t, err.Error(), MinimumStackVersion)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
