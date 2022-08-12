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
	. "github.com/numary/formance-operator/internal/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeScopesProgressing = "Progressing"
)

type ScopeCondition struct {
	// type of condition in CamelCase or in foo.example.com/CamelCase.
	// ---
	// Many .condition.type values are consistent across resources like Available, but because arbitrary conditions can be
	// useful (see .node.status.conditions), the ability to deconflict is important.
	// The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`
	// status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// observedGeneration represents the .metadata.generation that the condition was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9, the condition is out of date
	// with respect to the current state of the instance.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`
	// lastTransitionTime is the last time the condition transitioned from one status to another.
	// This should be when the underlying condition changed.  If that is not known, then using the time when the API field changed is acceptable.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,4,opt,name=lastTransitionTime"`
}

func (in ScopeCondition) GetType() string {
	return in.Type
}

func (in ScopeCondition) GetStatus() metav1.ConditionStatus {
	return in.Status
}

func (in ScopeCondition) GetObservedGeneration() int64 {
	return in.ObservedGeneration
}

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
	Conditions   []ScopeCondition                `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
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

func (in *Scope) GetConditions() []ScopeCondition {
	return in.Status.Conditions
}

func (in *Scope) Condition(conditionType string) *ScopeCondition {
	return First(in.Status.Conditions, func(c ScopeCondition) bool {
		return c.Type == conditionType
	})
}

func (s *Scope) AuthServerReference() string {
	return s.Spec.AuthServerReference
}

func (s *Scope) setCondition(c ScopeCondition) {
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
	s.setCondition(ScopeCondition{
		Type:               ConditionTypeScopesProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	})
}

func (s *Scope) StopProgression() {
	s.setCondition(ScopeCondition{
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
