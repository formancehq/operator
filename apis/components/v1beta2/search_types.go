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
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/apis/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SearchPostgresConfigs struct {
	Ledger apisv1beta1.PostgresConfigWithDatabase `json:"ledger"`
}

func (c SearchPostgresConfigs) Env() []corev1.EnvVar {
	return c.Ledger.EnvWithDiscriminator("", "LEDGER")
}

// SearchSpec defines the desired state of Search
type SearchSpec struct {
	CommonServiceProperties `json:",inline"`
	apisv1beta1.Scalable    `json:",inline"`

	// +optional
	Ingress *IngressSpec `json:"ingress"`
	// +optional
	Monitoring      *apisv1beta1.MonitoringSpec           `json:"monitoring"`
	ElasticSearch   componentsv1beta1.ElasticSearchConfig `json:"elasticsearch"`
	KafkaConfig     apisv1beta1.KafkaConfig               `json:"kafka"`
	Index           string                                `json:"index"`
	Batching        componentsv1beta1.Batching            `json:"batching"`
	PostgresConfigs SearchPostgresConfigs                 `json:"postgres"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
//+kubebuilder:storageversion

// Search is the Schema for the searches API
type Search struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SearchSpec                    `json:"spec,omitempty"`
	Status apisv1beta1.ReplicationStatus `json:"status,omitempty"`
}

func (in *Search) GetStatus() apisv1beta1.Dirty {
	return &in.Status
}

func (in *Search) IsDirty(t apisv1beta1.Object) bool {
	return false
}

func (in *Search) GetConditions() *apisv1beta1.Conditions {
	return &in.Status.Conditions
}

//+kubebuilder:object:root=true

// SearchList contains a list of Search
type SearchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Search `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Search{}, &SearchList{})
}
