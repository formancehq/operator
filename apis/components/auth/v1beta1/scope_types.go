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
	"time"

	"github.com/numary/auth/authclient"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeScopesProgressing = "Progressing"
)

// ScopeSpec defines the desired state of Scope
type ScopeSpec struct {
	Label string `json:"label"`
	// +optional
	Transient           []string `json:"transient"`
	AuthServerReference string   `json:"authServerReference"`
}

type TransientScopeStatus struct {
	ObservedGeneration int64  `json:"observedGeneration"`
	AuthServerID       string `json:"authServerID"`
	Date               string `json:"date"`
}

// ScopeStatus defines the observed state of Scope
type ScopeStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions   []Condition                     `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	AuthServerID string                          `json:"authServerID,omitempty"`
	Transient    map[string]TransientScopeStatus `json:"transient,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Server ID",type="string",JSONPath=".status.authServerID",description="Auth server ID"

// Scope is the Schema for the scopes API
type Scope struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScopeSpec   `json:"spec,omitempty"`
	Status ScopeStatus `json:"status,omitempty"`
}

func (in *Scope) GetConditions() []Condition {
	return in.Status.Conditions
}

func (in *Scope) Condition(conditionType string) *Condition {
	return First(in.Status.Conditions, func(c Condition) bool {
		return c.Type == conditionType
	})
}

func (s *Scope) AuthServerReference() string {
	return s.Spec.AuthServerReference
}

func (s *Scope) setCondition(c Condition) {
	c.ObservedGeneration = s.Generation
	for ind, condition := range s.Status.Conditions {
		if condition.Type == c.Type {
			s.Status.Conditions[ind] = c
			return
		}
	}
	s.Status.Conditions = append(s.Status.Conditions, c)
}

func (s *Scope) Progress() {
	s.setCondition(Condition{
		Type:               ConditionTypeScopesProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	})
}

func (s *Scope) StopProgression() {
	s.setCondition(Condition{
		Type:               ConditionTypeScopesProgressing,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
	})
}

func (s *Scope) IsInTransient(authScope *authclient.Scope) bool {
	return First(authScope.Transient, Equal(s.Status.AuthServerID)) != nil
}

func (s *Scope) IsCreatedOnAuthServer() bool {
	return s.Status.AuthServerID != ""
}

func (s *Scope) ClearAuthServerID() {
	s.Status.AuthServerID = ""
}

func (s *Scope) SetRegisteredTransientScope(transientScope *Scope) {
	if s.Status.Transient == nil {
		s.Status.Transient = map[string]TransientScopeStatus{}
	}
	s.Status.Transient[transientScope.Name] = TransientScopeStatus{
		ObservedGeneration: transientScope.Generation,
		AuthServerID:       transientScope.Status.AuthServerID,
		Date:               time.Now().Format(time.RFC3339),
	}
}

func NewScope(name, label string, transient ...string) *Scope {
	return &Scope{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ScopeSpec{
			Label:     label,
			Transient: transient,
		},
	}
}

//+kubebuilder:object:root=true

// ScopeList contains a list of Scope
type ScopeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Scope `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Scope{}, &ScopeList{})
}
