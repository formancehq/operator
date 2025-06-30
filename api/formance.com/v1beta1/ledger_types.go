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

type LedgerSpec struct {
	ModuleProperties `json:",inline"`
	StackDependency  `json:",inline"`
	// +optional
	Auth *AuthConfig `json:"auth,omitempty"`
}

type LedgerStatus struct {
	Status `json:",inline"`
}

// Ledger is the module allowing to install a ledger instance.
//
// The ledger is a stateful application that manages financial transactions
// and maintains an immutable audit trail.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Stack",type=string,JSONPath=".spec.stack",description="Stack"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.ready",description="Is ready"
// +kubebuilder:printcolumn:name="Info",type=string,JSONPath=".status.info",description="Info"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version",description="Version"
// +kubebuilder:metadata:labels=formance.com/kind=module
type Ledger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LedgerSpec   `json:"spec,omitempty"`
	Status LedgerStatus `json:"status,omitempty"`
}

func (in *Ledger) IsEE() bool {
	return false
}

func (in *Ledger) IsReady() bool {
	return in.Status.Ready
}

func (in *Ledger) SetReady(b bool) {
	in.Status.Ready = b
}

func (in *Ledger) SetError(s string) {
	in.Status.Info = s
}

func (in *Ledger) GetConditions() *Conditions {
	return &in.Status.Conditions
}

func (in *Ledger) GetVersion() string {
	return in.Spec.Version
}

func (a Ledger) isEventPublisher() {}

func (a Ledger) GetStack() string {
	return a.Spec.Stack
}

func (a Ledger) IsDebug() bool {
	return a.Spec.Debug
}

func (a Ledger) IsDev() bool {
	return a.Spec.Dev
}

//+kubebuilder:object:root=true

// LedgerList contains a list of Ledger
type LedgerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Ledger `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Ledger{}, &LedgerList{})
}
