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
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CounterpartiesSpec defines the desired state of Counterparties
type CounterpartiesSpec struct {
	CommonServiceProperties `json:",inline"`

	// +optional
	Postgres componentsv1beta1.PostgresConfigCreateDatabase `json:"postgres"`
	// +optional
	Ingress *IngressSpec `json:"ingress"`
	// +optional
	Monitoring *apisv1beta2.MonitoringSpec `json:"monitoring"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion

// Counterparties is the Schema for the Counterparties API
type Counterparties struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CounterpartiesSpec `json:"spec,omitempty"`
	Status apisv1beta1.Status `json:"status,omitempty"`
}

func (in *Counterparties) GetStatus() apisv1beta1.Dirty {
	return &in.Status
}

func (in *Counterparties) GetConditions() *apisv1beta1.Conditions {
	return &in.Status.Conditions
}

func (in *Counterparties) IsDirty(t apisv1beta1.Object) bool {
	return false
}

//+kubebuilder:object:root=true

// CounterpartiesList contains a list of Counterparties
type CounterpartiesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Counterparties `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Counterparties{}, &CounterpartiesList{})
}
