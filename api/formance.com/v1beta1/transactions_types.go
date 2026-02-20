/*
Copyright 2023.

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

type TransactionsSpec struct {
	StackDependency  `json:",inline"`
	ModuleProperties `json:",inline"`
	// +optional
	Auth *AuthConfig `json:"auth,omitempty"`
}

type TransactionsStatus struct {
	Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Stack",type=string,JSONPath=".spec.stack",description="Stack"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.ready",description="Is ready"
// +kubebuilder:printcolumn:name="Info",type=string,JSONPath=".status.info",description="Info"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version",description="Version"
// +kubebuilder:metadata:labels=formance.com/kind=module

// Transactions is the Schema for the transactions API
type Transactions struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TransactionsSpec   `json:"spec,omitempty"`
	Status TransactionsStatus `json:"status,omitempty"`
}

func (in *Transactions) isEventPublisher() {}

func (in *Transactions) IsEE() bool {
	return false
}

func (in *Transactions) SetReady(b bool) {
	in.Status.Ready = b
}

func (in *Transactions) IsReady() bool {
	return in.Status.Ready
}

func (in *Transactions) SetError(s string) {
	in.Status.Info = s
}

func (in *Transactions) GetConditions() *Conditions {
	return &in.Status.Conditions
}

func (in *Transactions) GetVersion() string {
	return in.Spec.Version
}

func (a Transactions) GetStack() string {
	return a.Spec.Stack
}

func (a Transactions) IsDebug() bool {
	return a.Spec.Debug
}

func (a Transactions) IsDev() bool {
	return a.Spec.Dev
}

//+kubebuilder:object:root=true

// TransactionsList contains a list of Transactions
type TransactionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Transactions `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Transactions{}, &TransactionsList{})
}

var _ EventPublisher = (*Transactions)(nil)
