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
	"reflect"

	authcomponentsv1beta1 "github.com/numary/operator/apis/components/auth/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// +required
	Seed string `json:"seed"`
	// +required
	Version string `json:"version"`
	// +optional
	ConfigurationSpec `json:",inline"`

	// +optional
	Debug bool `json:"debug"`
	// +required
	Namespace string `json:"namespace,omitempty"`
	// +optional
	// +required
	Host string `json:"host"`
	// +optional
	Scheme string `json:"scheme"`
}

type ServicesSpec struct {
	// +optional
	Control *ControlSpec `json:"control,omitempty"`
	// +optional
	Ledger *LedgerSpec `json:"ledger,omitempty"`
	// +optional
	Payments *PaymentsSpec `json:"payments,omitempty"`
	// +optional
	Search *SearchSpec `json:"search,omitempty"`
	// +optional
	Webhooks *WebhooksSpec `json:"webhooks,omitempty"`
}

const (
	ConditionTypeStackNamespaceReady  = "NamespaceReady"
	ConditionTypeStackAuthReady       = "AuthReady"
	ConditionTypeStackLedgerReady     = "LedgerReady"
	ConditionTypeStackSearchReady     = "SearchReady"
	ConditionTypeStackControlReady    = "ControlReady"
	ConditionTypeStackPaymentsReady   = "PaymentsReady"
	ConditionTypeStackWebhooksReady   = "WebhooksReady"
	ConditionTypeStackMiddlewareReady = "MiddlewareReady"
)

type ControlAuthentication struct {
	ClientID string
}

type StackStatus struct {
	Status `json:",inline"`

	// +optional
	StaticAuthClients map[string]authcomponentsv1beta1.StaticClient `json:"staticAuthClients,omitempty"`
}

func (s *StackStatus) IsDirty(reference Object) bool {
	if s.Status.IsDirty(reference) {
		return true
	}
	return !reflect.DeepEqual(reference.(*Stack).Status.StaticAuthClients, s.StaticAuthClients)
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.progress`
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="Stack Version"
// +kubebuilder:printcolumn:name="Configuration",type="string",JSONPath=".spec.seed",description="Stack Configuration"
// +kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".spec.namespace",description="Stack Namespace"

// Stack is the Schema for the stacks API
type Stack struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StackSpec   `json:"spec,omitempty"`
	Status StackStatus `json:"status,omitempty"`
}

func (s *Stack) GetScheme() string {
	if s.Spec.Scheme != "" {
		return s.Spec.Scheme
	}
	return "https"
}

func (s *Stack) URL() string {
	return fmt.Sprintf("%s://%s", s.GetScheme(), s.Spec.Host)
}

func (s *Stack) GetStatus() Dirty {
	return &s.Status
}

func (s *Stack) IsDirty(t Object) bool {
	return false
}

func (s *Stack) GetConditions() *Conditions {
	return &s.Status.Conditions
}

func (s *Stack) SetNamespaceCreated() {
	SetCondition(s, ConditionTypeStackNamespaceReady, metav1.ConditionTrue)
}

func (s *Stack) SetNamespaceError(msg string) {
	SetCondition(s, ConditionTypeStackNamespaceReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetAuthReady() {
	SetCondition(s, ConditionTypeStackAuthReady, metav1.ConditionTrue)
}

func (s *Stack) SetAuthError(msg string) {
	SetCondition(s, ConditionTypeStackAuthReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetLedgerReady() {
	SetCondition(s, ConditionTypeStackLedgerReady, metav1.ConditionTrue)
}

func (s *Stack) SetLedgerError(msg string) {
	SetCondition(s, ConditionTypeStackLedgerReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetSearchReady() {
	SetCondition(s, ConditionTypeStackSearchReady, metav1.ConditionTrue)
}

func (s *Stack) SetSearchError(msg string) {
	SetCondition(s, ConditionTypeStackSearchReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetControlReady() {
	SetCondition(s, ConditionTypeStackControlReady, metav1.ConditionTrue)
}

func (s *Stack) SetControlError(msg string) {
	SetCondition(s, ConditionTypeStackControlReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetPaymentError(msg string) {
	SetCondition(s, ConditionTypeStackPaymentsReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetWebhooksError(msg string) {
	SetCondition(s, ConditionTypeStackWebhooksReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetMiddlewareError(msg string) {
	SetCondition(s, ConditionTypeStackMiddlewareReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetPaymentReady() {
	SetCondition(s, ConditionTypeStackPaymentsReady, metav1.ConditionTrue)
}

func (s *Stack) SetWebhooksReady() {
	SetCondition(s, ConditionTypeStackWebhooksReady, metav1.ConditionTrue)
}

func (s *Stack) RemoveAuthStatus() {
	s.Status.RemoveCondition(ConditionTypeStackAuthReady)
}

func (s *Stack) RemoveSearchStatus() {
	s.Status.RemoveCondition(ConditionTypeStackSearchReady)
}

func (s *Stack) RemoveControlStatus() {
	s.Status.RemoveCondition(ConditionTypeStackControlReady)
}

func (in *Stack) RemovePaymentsStatus() {
	in.Status.RemoveCondition(ConditionTypeStackPaymentsReady)
}

func (in *Stack) RemoveWebhooksStatus() {
	in.Status.RemoveCondition(ConditionTypeStackWebhooksReady)
}

func (in *Stack) SetMiddlewareReady() {
	in.Status.RemoveCondition(ConditionTypeStackMiddlewareReady)
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
