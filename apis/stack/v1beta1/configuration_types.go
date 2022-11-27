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
	. "github.com/formancehq/operator/apis/sharedtypes"
	. "github.com/formancehq/operator/internal/collectionutil"
	"github.com/imdario/mergo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
	return MergeAll(
		Map(in.Services.Ledger.Validate(), AddPrefixToFieldError("services.ledger")),
		Map(in.Services.Payments.Validate(), AddPrefixToFieldError("services.payments")),
		Map(in.Services.Search.Validate(), AddPrefixToFieldError("services.search")),
		Map(in.Services.Webhooks.Validate(), AddPrefixToFieldError("services.webhooks")),
		Map(in.Auth.Validate(), AddPrefixToFieldError("auth")),
		Map(in.Monitoring.Validate(), AddPrefixToFieldError("monitoring")),
		Map(in.Kafka.Validate(), AddPrefixToFieldError("kafka")),
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
