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
	. "github.com/numary/formance-operator/internal/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeStreamProgressing = "Progressing"
	ConditionTypeStreamReady       = "Ready"
)

// StreamSpec defines the desired state of Stream
type StreamSpec struct {
	Reference string `json:"ref"`
	Config    string `json:"config"`
}

// StreamStatus defines the observed state of Stream
type StreamStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Stream is the Schema for the streams API
type Stream struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StreamSpec   `json:"spec,omitempty"`
	Status StreamStatus `json:"status,omitempty"`
}

func (in *Stream) GetConditions() []Condition {
	return in.Status.Conditions
}

func (in *Stream) setCondition(c Condition) {
	for ind, condition := range in.Status.Conditions {
		if condition.Type == c.Type {
			in.Status.Conditions[ind] = c
			return
		}
	}
	in.Status.Conditions = append(in.Status.Conditions, c)
}

func (in *Stream) removeCondition(v string) {
	in.Status.Conditions = Filter(in.Status.Conditions, func(stack Condition) bool {
		return stack.Type != v
	})
}

func (in *Stream) SetProgressing() {
	in.removeCondition(ConditionTypeStreamReady)
	in.setCondition(Condition{
		Type:               ConditionTypeStreamProgressing,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: in.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Stream) SetReady() {
	in.removeCondition(ConditionTypeStreamProgressing)
	in.setCondition(Condition{
		Type:               ConditionTypeStreamReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: in.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

//+kubebuilder:object:root=true

// StreamList contains a list of Stream
type StreamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Stream `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Stream{}, &StreamList{})
}
