package v1beta1

import (
	"testing"

	"github.com/formancehq/operator/apis/sharedtypes"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
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
				Auth: &AuthSpec{
					ImageHolder: sharedtypes.ImageHolder{
						ImagePullSecrets: []v1.LocalObjectReference{{
							Name: "ref1",
						}},
					},
				},
			},
			override: &ConfigurationSpec{
				Auth: &AuthSpec{
					ImageHolder: sharedtypes.ImageHolder{
						ImagePullSecrets: []v1.LocalObjectReference{{
							Name: "ref2",
						}},
					},
				},
			},
			merged: &ConfigurationSpec{
				Auth: &AuthSpec{
					ImageHolder: sharedtypes.ImageHolder{
						ImagePullSecrets: []v1.LocalObjectReference{{
							Name: "ref1",
						}, {
							Name: "ref2",
						}},
					},
				},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.merged, testCase.src.MergeWith(testCase.override))
		})
	}

}
