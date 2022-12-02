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
	"fmt"
	"reflect"
	"strings"

	authcomponentsv1beta1 "github.com/numary/operator/apis/auth.components/v1beta1"
	"github.com/numary/operator/apis/components/v1beta1"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/apis/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IngressGlobalConfig struct {
	IngressConfig `json:",inline"`
	// +optional
	TLS *apisv1beta1.IngressTLS `json:"tls"`
}

type StackAuthSpec struct {
	DelegatedOIDCServer v1beta1.DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`
	// +optional
	StaticClients []authcomponentsv1beta1.StaticClient `json:"staticClients,omitempty"`
}

// StackSpec defines the desired state of Stack
type StackSpec struct {
	DevProperties `json:",inline"`
	Seed          string        `json:"seed"`
	Host          string        `json:"host"`
	Auth          StackAuthSpec `json:"auth"`

	// +optional
	Versions string `json:"versions"`

	// +optional
	// +kubebuilder:default:="http"
	Scheme string `json:"scheme"`
}

const (
	ConditionTypeStackNamespaceReady  = "NamespaceReady"
	ConditionTypeStackAuthReady       = "AuthReady"
	ConditionTypeStackLedgerReady     = "LedgerReady"
	ConditionTypeStackSearchReady     = "SearchReady"
	ConditionTypeStackControlReady    = "ControlReady"
	ConditionTypeStackPaymentsReady   = "PaymentsReady"
	ConditionTypeStackWebhooksReady   = "WebhooksReady"
	ConditionTypeStackNextReady       = "NextReady"
	ConditionTypeStackMiddlewareReady = "MiddlewareReady"
)

type ControlAuthentication struct {
	ClientID string
}

type StackStatus struct {
	apisv1beta1.Status `json:",inline"`

	// +optional
	StaticAuthClients map[string]authcomponentsv1beta1.StaticClient `json:"staticAuthClients,omitempty"`
}

func (s *StackStatus) IsDirty(reference apisv1beta1.Object) bool {
	if s.Status.IsDirty(reference) {
		return true
	}
	return !reflect.DeepEqual(reference.(*Stack).Status.StaticAuthClients, s.StaticAuthClients)
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.progress`
//+kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.versions",description="Stack Version"
//+kubebuilder:printcolumn:name="Configuration",type="string",JSONPath=".spec.seed",description="Stack Configuration"
//+kubebuilder:storageversion

// Stack is the Schema for the stacks API
type Stack struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StackSpec   `json:"spec,omitempty"`
	Status StackStatus `json:"status,omitempty"`
}

func NewStack(name string, spec StackSpec) Stack {
	return Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: spec,
	}
}

func (*Stack) Hub() {}

func (s *Stack) GetScheme() string {
	if s.Spec.Scheme != "" {
		return s.Spec.Scheme
	}
	return "https"
}

func (s *Stack) URL() string {
	return fmt.Sprintf("%s://%s", s.GetScheme(), s.Spec.Host)
}

func (s *Stack) GetStatus() apisv1beta1.Dirty {
	return &s.Status
}

func (s *Stack) IsDirty(t apisv1beta1.Object) bool {
	return false
}

func (s *Stack) GetConditions() *apisv1beta1.Conditions {
	return &s.Status.Conditions
}

func (s *Stack) SetNamespaceCreated() {
	apisv1beta1.SetCondition(s, ConditionTypeStackNamespaceReady, metav1.ConditionTrue)
}

func (s *Stack) SetNamespaceError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackNamespaceReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetAuthReady() {
	apisv1beta1.SetCondition(s, ConditionTypeStackAuthReady, metav1.ConditionTrue)
}

func (s *Stack) SetAuthError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackAuthReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetLedgerReady() {
	apisv1beta1.SetCondition(s, ConditionTypeStackLedgerReady, metav1.ConditionTrue)
}

func (s *Stack) SetNextReady() {
	apisv1beta1.SetCondition(s, ConditionTypeStackNextReady, metav1.ConditionTrue)
}

func (s *Stack) SetLedgerError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackLedgerReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetNextError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackNextReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetSearchReady() {
	apisv1beta1.SetCondition(s, ConditionTypeStackSearchReady, metav1.ConditionTrue)
}

func (s *Stack) SetSearchError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackSearchReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetControlReady() {
	apisv1beta1.SetCondition(s, ConditionTypeStackControlReady, metav1.ConditionTrue)
}

func (s *Stack) SetControlError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackControlReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetPaymentError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackPaymentsReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetWebhooksError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackWebhooksReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetMiddlewareError(msg string) {
	apisv1beta1.SetCondition(s, ConditionTypeStackMiddlewareReady, metav1.ConditionFalse, msg)
}

func (s *Stack) SetPaymentReady() {
	apisv1beta1.SetCondition(s, ConditionTypeStackPaymentsReady, metav1.ConditionTrue)
}

func (s *Stack) SetWebhooksReady() {
	apisv1beta1.SetCondition(s, ConditionTypeStackWebhooksReady, metav1.ConditionTrue)
}

func (s *Stack) RemoveAuthStatus() {
	s.Status.RemoveCondition(ConditionTypeStackAuthReady)
}

func (s *Stack) RemoveSearchStatus() {
	s.Status.RemoveCondition(ConditionTypeStackSearchReady)
}

func (s *Stack) RemoveNextStatus() {
	s.Status.RemoveCondition(ConditionTypeStackNextReady)
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
	return fmt.Sprintf("%s-%s", s.Name, strings.ToLower(v))
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
