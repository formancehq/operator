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

package v1beta2

import (
	"github.com/numary/operator/pkg/apis/v1beta1"
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	"github.com/numary/operator/pkg/typeutils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type ConfigurationServicesSpec struct {
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`
	// +optional
	Control *ControlSpec `json:"control,omitempty"`
	// +optional
	Ledger *LedgerSpec `json:"ledger,omitempty"`
	// +optional
	Payments *PaymentsSpec `json:"payments,omitempty"`
	// +optional
	Search *SearchSpec `json:"search,omitempty"`
	// +optional
	Webhooks *WebhooksSpec `json:"webhooks,omitempty"`
}

type ConfigurationSpec struct {
	// +optional
	Monitoring *apisv1beta2.MonitoringSpec `json:"monitoring,omitempty"`
	// +optional
	Services ConfigurationServicesSpec `json:"services,omitempty"`
	// +optional
	Ingress *IngressGlobalConfig `json:"ingress,omitempty"`
	// +optional
	Kafka *apisv1beta2.KafkaConfig `json:"kafka,omitempty"`
}

func (in *ConfigurationSpec) Validate() field.ErrorList {
	return typeutils.MergeAll(
		typeutils.Map(in.Services.Ledger.Validate(), v1beta1.AddPrefixToFieldError("services.ledger")),
		typeutils.Map(in.Services.Payments.Validate(), v1beta1.AddPrefixToFieldError("services.payments")),
		typeutils.Map(in.Services.Search.Validate(), v1beta1.AddPrefixToFieldError("services.search")),
		typeutils.Map(in.Services.Webhooks.Validate(), v1beta1.AddPrefixToFieldError("services.webhooks")),
		typeutils.Map(in.Services.Auth.Validate(), v1beta1.AddPrefixToFieldError("services.auth")),
		typeutils.Map(in.Monitoring.Validate(), v1beta1.AddPrefixToFieldError("monitoring")),
		typeutils.Map(in.Kafka.Validate(), v1beta1.AddPrefixToFieldError("kafka")),
	)
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status
//+kubebuilder:storageversion

// Configuration is the Schema for the configurations API
type Configuration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigurationSpec  `json:"spec,omitempty"`
	Status apisv1beta2.Status `json:"status,omitempty"`
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
