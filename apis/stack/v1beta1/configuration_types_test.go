package v1beta1

import (
	"testing"

	"github.com/numary/operator/apis/sharedtypes"
	"github.com/stretchr/testify/require"
)

func TestConfigurationOverride(t *testing.T) {

	type testCase struct {
		name     string
		src      *ConfigurationSpec
		override *ConfigurationSpec
		merged   *ConfigurationSpec
	}

	for _, testCase := range []testCase{
		{
			name:     "nominal",
			src:      &ConfigurationSpec{},
			override: &ConfigurationSpec{},
			merged:   &ConfigurationSpec{},
		},
		{
			name: "override",
			src: &ConfigurationSpec{
				Monitoring: &sharedtypes.MonitoringSpec{
					Traces: &sharedtypes.TracesSpec{
						Otlp: &sharedtypes.TracesOtlpSpec{
							Endpoint: "remote",
						},
					},
				},
			},
			override: &ConfigurationSpec{
				Monitoring: &sharedtypes.MonitoringSpec{
					Traces: &sharedtypes.TracesSpec{
						Otlp: &sharedtypes.TracesOtlpSpec{
							Endpoint: "localhost",
						},
					},
				},
			},
			merged: &ConfigurationSpec{
				Monitoring: &sharedtypes.MonitoringSpec{
					Traces: &sharedtypes.TracesSpec{
						Otlp: &sharedtypes.TracesOtlpSpec{
							Endpoint: "localhost",
						},
					},
				},
			},
		},
		{
			name: "override array",
			src: &ConfigurationSpec{
				Auth: &AuthSpec{},
			},
			override: &ConfigurationSpec{
				Auth: &AuthSpec{},
			},
			merged: &ConfigurationSpec{
				Auth: &AuthSpec{},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.merged, testCase.src.MergeWith(testCase.override))
		})
	}

}
