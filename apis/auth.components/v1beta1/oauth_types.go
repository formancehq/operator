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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OAuthSpec defines the desired state of OAuth
type OAuthSpec struct {
}

// OAuthStatus defines the observed state of OAuth
type OAuthStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// OAuth is the Schema for the oauths API
type OAuth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OAuthSpec   `json:"spec,omitempty"`
	Status OAuthStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OAuthList contains a list of OAuth
type OAuthList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OAuth `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OAuth{}, &OAuthList{})
}
