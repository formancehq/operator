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
	"github.com/numary/formance-operator/internal/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeServerProgressing       = "Progressing"
	ConditionTypeServerDeploymentCreated = "DeploymentCreated"
	ConditionTypeServerServiceCreated    = "ServiceCreated"
	ConditionTypeServerReady             = "Ready"
)

type ServerCondition struct {
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

func (in ServerCondition) GetType() string {
	return in.Type
}

func (in ServerCondition) GetStatus() metav1.ConditionStatus {
	return in.Status
}

func (in ServerCondition) GetObservedGeneration() int64 {
	return in.ObservedGeneration
}

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
	Conditions []ServerCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	Ready      bool              `json:"ready"`
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

func (in *Server) GetConditions() []ServerCondition {
	return in.Status.Conditions
}

func (in *Server) setCondition(c ServerCondition) {
	for ind, condition := range in.Status.Conditions {
		if condition.Type == c.Type {
			in.Status.Conditions[ind] = c
			return
		}
	}
	in.Status.Conditions = append(in.Status.Conditions, c)
}

func (in *Server) Progress() {
	in.setCondition(ServerCondition{
		Type:               ConditionTypeServerProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.setCondition(ServerCondition{
		Type:               ConditionTypeServerReady,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.Status.Ready = false
}

func (in *Server) removeCondition(v string) {
	in.Status.Conditions = collectionutil.Filter(in.Status.Conditions, func(stack ServerCondition) bool {
		return stack.Type != v
	})
}

func (in *Server) SetReady() {
	in.removeCondition(ConditionTypeServerProgressing)
	in.setCondition(ServerCondition{
		Type:               ConditionTypeServerReady,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.Status.Ready = true
}

func (a *Server) SetDeploymentCreated() {
	a.setCondition(ServerCondition{
		Type:               ConditionTypeServerDeploymentCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Server) SetDeploymentFailure(err error) {
	a.setCondition(ServerCondition{
		Type:               ConditionTypeServerDeploymentCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Server) SetServiceCreated() {
	a.setCondition(ServerCondition{
		Type:               ConditionTypeServerServiceCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Server) SetServiceFailure(err error) {
	a.setCondition(ServerCondition{
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
