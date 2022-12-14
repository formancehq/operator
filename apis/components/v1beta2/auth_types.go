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
	authv1beta1 "github.com/numary/operator/apis/auth.components/v1beta2"
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/apis/v1beta2"
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AuthSpec defines the desired state of Auth
type AuthSpec struct {
	CommonServiceProperties `json:",inline"`
	apisv1beta1.Scalable    `json:",inline"`
	Postgres                componentsv1beta1.PostgresConfigCreateDatabase `json:"postgres"`
	BaseURL                 string                                         `json:"baseURL"`

	// SigningKey is a private key
	// The signing key is used by the server to sign JWT tokens
	// The value of this config will be copied to a secret and injected inside
	// the env vars of the server using secret mapping.
	// If not specified, a key will be automatically generated.
	// +optional
	SigningKey string `json:"signingKey"`
	// +optional
	Ingress *IngressSpec `json:"ingress"`

	DelegatedOIDCServer componentsv1beta1.DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`

	// +optional
	Monitoring *apisv1beta2.MonitoringSpec `json:"monitoring"`

	// +optional
	StaticClients []authv1beta1.StaticClient `json:"staticClients"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
//+kubebuilder:storageversion

// Auth is the Schema for the auths API
type Auth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuthSpec                      `json:"spec,omitempty"`
	Status apisv1beta1.ReplicationStatus `json:"status,omitempty"`
}

func (a *Auth) GetStatus() apisv1beta1.Dirty {
	return &a.Status
}

func (a *Auth) IsDirty(t apisv1beta1.Object) bool {
	return false
}

func (a *Auth) GetConditions() *apisv1beta1.Conditions {
	return &a.Status.Conditions
}

func (in *Auth) HasStaticClients() bool {
	return in.Spec.StaticClients != nil && len(in.Spec.StaticClients) > 0
}

//+kubebuilder:object:root=true

// AuthList contains a list of Auth
type AuthList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Auth `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Auth{}, &AuthList{})
}
