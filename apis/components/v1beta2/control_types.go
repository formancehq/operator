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
	. "github.com/numary/operator/pkg/apis/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AuthClientConfiguration struct {
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret"`
}

// ControlSpec defines the desired state of Control
type ControlSpec struct {
	CommonServiceProperties `json:",inline"`
	v1beta1.Scalable        `json:",inline"`

	// +optional
	Ingress *IngressSpec `json:"ingress"`
	// +optional
	Monitoring  *v1beta1.MonitoringSpec `json:"monitoring"`
	ApiURLFront string                  `json:"apiURLFront"`
	ApiURLBack  string                  `json:"apiURLBack"`

	// +optional
	AuthClientConfiguration *AuthClientConfiguration `json:"auth"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
//+kubebuilder:storageversion

// Control is the Schema for the controls API
type Control struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ControlSpec               `json:"spec,omitempty"`
	Status v1beta1.ReplicationStatus `json:"status,omitempty"`
}

func (in *Control) GetStatus() v1beta1.Dirty {
	return &in.Status
}

func (in *Control) IsDirty(t v1beta1.Object) bool {
	return false
}

func (in *Control) GetConditions() *v1beta1.Conditions {
	return &in.Status.Conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector

// ControlList contains a list of Control
type ControlList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Control `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Control{}, &ControlList{})
}
