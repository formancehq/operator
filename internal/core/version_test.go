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
		{name: "v2.0.0 rejected", version: "v2.0.0", wantError: true},
		{name: "v2.1.0 rejected", version: "v2.1.0", wantError: true},
		{name: "v2.1.9 rejected", version: "v2.1.9", wantError: true},
		{name: "v2.0.0-rc.5 rejected", version: "v2.0.0-rc.5", wantError: true},
		{name: "v2.2.0-alpha pre-release rejected", version: "v2.2.0-alpha", wantError: true},
		{name: "v2.2.0 accepted", version: "v2.2.0", wantError: false},
		{name: "v2.3.0 accepted", version: "v2.3.0", wantError: false},
		{name: "v3.0.0 accepted", version: "v3.0.0", wantError: false},
		{name: "non-semver accepted", version: "main", wantError: false},
		{name: "sha ref accepted", version: "abc123def", wantError: false},
		{name: "latest accepted", version: "latest", wantError: false},
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
