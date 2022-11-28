package v1beta1

import (
	"testing"

	"github.com/numary/operator/pkg/apis/v1beta1"
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
				Monitoring: &v1beta1.MonitoringSpec{
					Traces: &v1beta1.TracesSpec{
						Otlp: &v1beta1.TracesOtlpSpec{
							Endpoint: "remote",
						},
					},
				},
			},
			override: &ConfigurationSpec{
				Monitoring: &v1beta1.MonitoringSpec{
					Traces: &v1beta1.TracesSpec{
						Otlp: &v1beta1.TracesOtlpSpec{
							Endpoint: "localhost",
						},
					},
				},
			},
			merged: &ConfigurationSpec{
				Monitoring: &v1beta1.MonitoringSpec{
					Traces: &v1beta1.TracesSpec{
						Otlp: &v1beta1.TracesOtlpSpec{
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
					ImageHolder: v1beta1.ImageHolder{
						ImagePullSecrets: []v1.LocalObjectReference{{
							Name: "ref1",
						}},
					},
				},
			},
			override: &ConfigurationSpec{
				Auth: &AuthSpec{
					ImageHolder: v1beta1.ImageHolder{
						ImagePullSecrets: []v1.LocalObjectReference{{
							Name: "ref2",
						}},
					},
				},
			},
			merged: &ConfigurationSpec{
				Auth: &AuthSpec{
					ImageHolder: v1beta1.ImageHolder{
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
