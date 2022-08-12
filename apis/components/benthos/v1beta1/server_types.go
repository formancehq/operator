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
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeServerProgressing       = "Progressing"
	ConditionTypeServerDeploymentCreated = "DeploymentCreated"
	ConditionTypeServerServiceCreated    = "ServiceCreated"
	ConditionTypeServerReady             = "Ready"
)

// ServerSpec defines the desired state of Server
type ServerSpec struct {
	// +optional
	Image string `json:"image"`
}

// ServerStatus defines the observed state of Server
type ServerStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	Ready      bool        `json:"ready"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Server is the Schema for the servers API
type Server struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServerSpec   `json:"spec,omitempty"`
	Status ServerStatus `json:"status,omitempty"`
}

func (in *Server) GetConditions() []Condition {
	return in.Status.Conditions
}

func (in *Server) setCondition(c Condition) {
	for ind, condition := range in.Status.Conditions {
		if condition.Type == c.Type {
			in.Status.Conditions[ind] = c
			return
		}
	}
	in.Status.Conditions = append(in.Status.Conditions, c)
}

func (in *Server) Progress() {
	in.setCondition(Condition{
		Type:               ConditionTypeServerProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.setCondition(Condition{
		Type:               ConditionTypeServerReady,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.Status.Ready = false
}

func (in *Server) removeCondition(v string) {
	in.Status.Conditions = collectionutil.Filter(in.Status.Conditions, func(stack Condition) bool {
		return stack.Type != v
	})
}

func (in *Server) SetReady() {
	in.removeCondition(ConditionTypeServerProgressing)
	in.setCondition(Condition{
		Type:               ConditionTypeServerReady,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.Status.Ready = true
}

func (a *Server) SetDeploymentCreated() {
	a.setCondition(Condition{
		Type:               ConditionTypeServerDeploymentCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Server) SetDeploymentFailure(err error) {
	a.setCondition(Condition{
		Type:               ConditionTypeServerDeploymentCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Server) SetServiceCreated() {
	a.setCondition(Condition{
		Type:               ConditionTypeServerServiceCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Server) SetServiceFailure(err error) {
	a.setCondition(Condition{
		Type:               ConditionTypeServerServiceCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

//+kubebuilder:object:root=true

// ServerList contains a list of Server
type ServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Server `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Server{}, &ServerList{})
}
