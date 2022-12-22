/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"github.com/formancehq/operator/apis/stack/v1beta2"
	. "github.com/formancehq/operator/pkg/apis/v1beta2"
	"github.com/formancehq/operator/pkg/typeutils"
	"github.com/imdario/mergo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

type ConfigurationSpec struct {
	// +optional
	Monitoring *MonitoringSpec `json:"monitoring,omitempty"`
	// +optional
	Services ServicesSpec `json:"services,omitempty"`
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`
	// +optional
	Ingress IngressGlobalConfig `json:"ingress"`
	// +optional
	Kafka *KafkaConfig `json:"kafka"`
}

func (in *ConfigurationSpec) MergeWith(spec *ConfigurationSpec) *ConfigurationSpec {
	cp := in.DeepCopy()
	if err := mergo.Merge(cp, spec, mergo.WithOverride, mergo.WithAppendSlice); err != nil {
		panic(err)
	}
	return cp
}

func (in *ConfigurationSpec) Validate() field.ErrorList {
	return typeutils.MergeAll(
		typeutils.Map(in.Services.Ledger.Validate(), AddPrefixToFieldError("services.ledger")),
		typeutils.Map(in.Services.Payments.Validate(), AddPrefixToFieldError("services.payments")),
		typeutils.Map(in.Services.Search.Validate(), AddPrefixToFieldError("services.search")),
		typeutils.Map(in.Services.Webhooks.Validate(), AddPrefixToFieldError("services.webhooks")),
		typeutils.Map(in.Auth.Validate(), AddPrefixToFieldError("auth")),
		typeutils.Map(in.Monitoring.Validate(), AddPrefixToFieldError("monitoring")),
		typeutils.Map(in.Kafka.Validate(), AddPrefixToFieldError("kafka")),
	)
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status

// Configuration is the Schema for the configurations API
type Configuration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigurationSpec `json:"spec,omitempty"`
	Status Status            `json:"status,omitempty"`
}

func (src *Configuration) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.Configuration)
	typeutils.MapObject(src, &dst)
	dst.APIVersion = v1beta2.GroupVersion.Identifier()
	dst.Spec.Services.Auth = v1beta2.AuthSpec{
		Postgres: src.Spec.Auth.Postgres,
		Ingress: func() *v1beta2.IngressConfig {
			if src.Spec.Auth.Ingress == nil {
				return nil
			}
			return &v1beta2.IngressConfig{
				Annotations: func() map[string]string {
					if src.Spec.Auth == nil || src.Spec.Auth.Ingress == nil {
						return map[string]string{}
					}
					return src.Spec.Auth.Ingress.Annotations
				}(),
			}
		}(),
		StaticClients: src.Spec.Auth.StaticClients,
	}

	return nil
}

func (dst *Configuration) ConvertFrom(srcRaw conversion.Hub) error {
	typeutils.MapObject(srcRaw, &dst)
	src := srcRaw.(*v1beta2.Configuration)
	dst.APIVersion = GroupVersion.Identifier()
	dst.Spec.Auth = &AuthSpec{
		Postgres: src.Spec.Services.Auth.Postgres,
		Ingress: &IngressConfig{
			Enabled: pointer.Bool(true),
			Annotations: func() map[string]string {
				if src.Spec.Services.Auth.Ingress == nil {
					return map[string]string{}
				}
				return src.Spec.Services.Auth.Ingress.Annotations
			}(),
		},
		StaticClients: src.Spec.Services.Auth.StaticClients,
	}

	return nil
}

//+kubebuilder:object:root=true

// ConfigurationList contains a list of Configuration
type ConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Configuration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Configuration{}, &ConfigurationList{})
}
