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
	"fmt"

	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/internal/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type IngressGlobalConfig struct {
	// +optional
	TLS *IngressTLS `json:"tls"`
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// StackSpec defines the desired state of Stack
type StackSpec struct {
	// +optional
	Debug bool `json:"debug"`
	// +required
	Namespace string `json:"namespace,omitempty"`
	// +optional
	Monitoring *MonitoringSpec `json:"monitoring,omitempty"`
	// +optional
	Services ServicesSpec `json:"services,omitempty"`
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`
	// +optional
	Ingress IngressGlobalConfig `json:"ingress"`
	// +optional
	Kafka *KafkaConfig `json:"kafka"`
	// +required
	Host string `json:"host"`
	// +optional
	Scheme string `json:"scheme"`
}

func (in *StackSpec) Validate() field.ErrorList {
	return MergeAll(
		Map(in.Services.Ledger.Validate(), AddPrefixToFieldError("services.ledger")),
		Map(in.Services.Payments.Validate(), AddPrefixToFieldError("services.payments")),
		Map(in.Services.Search.Validate(), AddPrefixToFieldError("services.search")),
		Map(in.Auth.Validate(), AddPrefixToFieldError("auth")),
		Map(in.Monitoring.Validate(), AddPrefixToFieldError("monitoring")),
		Map(in.Kafka.Validate(), AddPrefixToFieldError("kafka")),
	)
}

type ServicesSpec struct {
	Control  *ControlSpec  `json:"control,omitempty"`
	Ledger   *LedgerSpec   `json:"ledger,omitempty"`
	Payments *PaymentsSpec `json:"payments,omitempty"`
	Search   *SearchSpec   `json:"search,omitempty"`
}

const (
	ConditionTypeStackNamespaceReady   = "NamespaceReady"
	ConditionTypeStackAuthReady        = "AuthReady"
	ConditionTypeStackLedgerReady      = "LedgerReady"
	ConditionTypeStackSearchReady      = "SearchReady"
	ConditionTypeStackControlReady     = "ControlReady"
	ConditionTypeStackPaymentsReady    = "PaymentsReady"
	ConditionTypeStackCertificateReady = "CertificateReady"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.progress`
//+kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="Stack Version"
//+kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".spec.namespace",description="Stack Namespace"

// Stack is the Schema for the stacks API
type Stack struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StackSpec `json:"spec,omitempty"`
	Status Status    `json:"status,omitempty"`
}

func (in *Stack) GetScheme() string {
	if in.Spec.Scheme != "" {
		return in.Spec.Scheme
	}
	return "https"
}
func (in *Stack) GetStatus() Dirty {
	return &in.Status
}

func (in *Stack) IsDirty(t Object) bool {
	return false
}

func (in *Stack) GetConditions() *Conditions {
	return &in.Status.Conditions
}

func (in *Stack) SetNamespaceCreated() {
	SetCondition(in, ConditionTypeStackNamespaceReady, metav1.ConditionTrue)
}

func (in *Stack) SetNamespaceError(msg string) {
	SetCondition(in, ConditionTypeStackNamespaceReady, metav1.ConditionFalse, msg)
}

func (in *Stack) SetAuthReady() {
	SetCondition(in, ConditionTypeStackAuthReady, metav1.ConditionTrue)
}

func (in *Stack) SetAuthError(msg string) {
	SetCondition(in, ConditionTypeStackAuthReady, metav1.ConditionFalse, msg)
}

func (in *Stack) SetLedgerReady() {
	SetCondition(in, ConditionTypeStackLedgerReady, metav1.ConditionTrue)
}

func (in *Stack) SetLedgerError(msg string) {
	SetCondition(in, ConditionTypeStackLedgerReady, metav1.ConditionFalse, msg)
}

func (in *Stack) SetSearchReady() {
	SetCondition(in, ConditionTypeStackSearchReady, metav1.ConditionTrue)
}

func (in *Stack) SetSearchError(msg string) {
	SetCondition(in, ConditionTypeStackSearchReady, metav1.ConditionFalse, msg)
}

func (in *Stack) SetControlReady() {
	SetCondition(in, ConditionTypeStackControlReady, metav1.ConditionTrue)
}

func (in *Stack) SetControlError(msg string) {
	SetCondition(in, ConditionTypeStackControlReady, metav1.ConditionFalse, msg)
}

func (in *Stack) SetPaymentError(msg string) {
	SetCondition(in, ConditionTypeStackPaymentsReady, metav1.ConditionFalse, msg)
}

func (in *Stack) SetPaymentReady() {
	SetCondition(in, ConditionTypeStackPaymentsReady, metav1.ConditionTrue)
}

func (in *Stack) SetCertificateReady() {
	SetCondition(in, ConditionTypeStackCertificateReady, metav1.ConditionTrue)
}

func (in *Stack) SetCertificateError(msg string) {
	SetCondition(in, ConditionTypeStackCertificateReady, metav1.ConditionFalse, msg)
}

func (in *Stack) RemoveAuthStatus() {
	in.Status.RemoveCondition(ConditionTypeStackAuthReady)
}

func (in *Stack) RemoveSearchStatus() {
	in.Status.RemoveCondition(ConditionTypeStackSearchReady)
}

func (in *Stack) RemoveControlStatus() {
	in.Status.RemoveCondition(ConditionTypeStackControlReady)
}

func (s *Stack) ServiceName(v string) string {
	return fmt.Sprintf("%s-%s", s.Name, v)
}

//+kubebuilder:object:root=true

// StackList contains a list of Stack
type StackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Stack `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Stack{}, &StackList{})
}
