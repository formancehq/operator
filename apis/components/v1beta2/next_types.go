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

// NextSpec defines the desired state of Next
type NextSpec struct {
	CommonServiceProperties `json:",inline"`
	apisv1beta1.Scalable    `json:",inline"`

	// +optional
	Ingress *IngressSpec `json:"ingress"`
	// +optional
	Postgres componentsv1beta1.PostgresConfigCreateDatabase `json:"postgres"`
	// +optional
	Monitoring *apisv1beta2.MonitoringSpec `json:"monitoring"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
//+kubebuilder:storageversion

// Next is the Schema for the nexts API
type Next struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NextSpec `json:"spec"`
	// +optional
	Status apisv1beta1.ReplicationStatus `json:"status"`
}

func (a *Next) GetStatus() apisv1beta1.Dirty {
	return &a.Status
}

func (a *Next) IsDirty(t apisv1beta1.Object) bool {
	return false
}

func (a *Next) GetConditions() *apisv1beta1.Conditions {
	return &a.Status.Conditions
}

//+kubebuilder:object:root=true

// NextList contains a list of Next
type NextList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Next `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Next{}, &NextList{})
}
