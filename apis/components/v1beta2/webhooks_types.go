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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WebhooksSpec defines the desired state of Webhooks
type WebhooksSpec struct {
	CommonServiceProperties `json:",inline"`

	Collector *componentsv1beta1.CollectorConfig `json:"collector"`
	MongoDB   apisv1beta1.MongoDBConfig          `json:"mongoDB"`
	// +optional
	Ingress *IngressSpec `json:"ingress"`
	// +optional
	Monitoring *apisv1beta1.MonitoringSpec `json:"monitoring"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion

// Webhooks is the Schema for the Webhooks API
type Webhooks struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WebhooksSpec       `json:"spec,omitempty"`
	Status apisv1beta1.Status `json:"status,omitempty"`
}

func (in *Webhooks) GetStatus() apisv1beta1.Dirty {
	return &in.Status
}

func (in *Webhooks) GetConditions() *apisv1beta1.Conditions {
	return &in.Status.Conditions
}

func (in *Webhooks) IsDirty(t apisv1beta1.Object) bool {
	return false
}

//+kubebuilder:object:root=true

// WebhooksList contains a list of Webhooks
type WebhooksList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Webhooks `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Webhooks{}, &WebhooksList{})
}