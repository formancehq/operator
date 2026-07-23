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

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"
)

const DefaultLedgerConfigurationName = "default"

type LedgerConfigurationSpec struct {
	// Stacks on which the configuration is applied. Can contain `*` to
	// indicate a wildcard, following the same convention as Settings.
	// +optional
	// +kubebuilder:validation:XValidation:rule="size(self) == 1 || !self.exists(stack, stack == '*')",message="the wildcard stack selector '*' cannot be combined with explicit stack names"
	Stacks []string `json:"stacks,omitempty"`

	// Cluster is the base Ledger v3 Cluster specification. Stack-specific
	// Settings and values owned by the Operator are applied on top of it.
	// +optional
	Cluster ledgerv1alpha1.ClusterSpec `json:"cluster,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// LedgerConfiguration defines the base specification applied to every Ledger v3
// Cluster targeted by spec.stacks. A configuration targeting a stack by name
// takes priority over a configuration targeting all stacks with `*`.
type LedgerConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec LedgerConfigurationSpec `json:"spec,omitempty"`
}

func (in *LedgerConfiguration) GetStacks() []string {
	return in.Spec.Stacks
}

func (in *LedgerConfiguration) IsWildcard() bool {
	return len(in.Spec.Stacks) == 1 && in.Spec.Stacks[0] == "*"
}

// +kubebuilder:object:root=true

// LedgerConfigurationList contains a list of LedgerConfiguration
type LedgerConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LedgerConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LedgerConfiguration{}, &LedgerConfigurationList{})
}
