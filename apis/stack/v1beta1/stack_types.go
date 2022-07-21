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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StackSpec defines the desired state of Stack
type StackSpec struct {
	// +required
	Version string `json:"version,omitempty"`
	// +required
	Namespace string `json:"namespace,omitempty"`
	// +optional
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`
	// +optional
	Services ServicesSpec `json:"services,omitempty"`
}

type MonitoringSpec struct {
	// +optional
	Traces TracesSpec `json:"traces,omitempty"`
}

type TracesSpec struct {
	// +optional
	Enabled bool           `json:"enabled,omitempty"`
	Otlp    TracesOtlpSpec `json:"otlp,omitempty"`
}

type TracesOtlpSpec struct {
	// +optional
	Enabled  bool   `json:"enabled,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

type ServicesSpec struct {
	Collector ServiceSpec `json:"collector,omitempty"`
	Control   ServiceSpec `json:"control,omitempty"`
	Ledger    ServiceSpec `json:"ledger,omitempty"`
	Payments  ServiceSpec `json:"payments,omitempty"`
	Search    ServiceSpec `json:"search,omitempty"`
}

type ServiceSpec struct {
	// +required
	Name string `json:"name,omitempty"`
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Auth AuthSpec `json:"auth,omitempty"`
	// +optional
	Databases []DatabaseSpec `json:"databases,omitempty"`
}

type ScalingSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	MinReplica int `json:"minReplica,omitempty"`
	// +optional
	MaxReplica int `json:"maxReplica,omitempty"`
	// +optional
	CpuLimit int `json:"cpuLimit,omitempty"`
}

type AuthSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Type string `json:"type,omitempty"`
}

type DatabaseSpec struct {
	// +optional
	Url string `json:"url,omitempty"`
	// +optional
	Type string `json:"type,omitempty"`
}

// StackProgress is a word summarizing the state of a Stack resource.
type StackProgress string

const (
	// StackProgressPending is Stack's status when it's waiting for the datacenter to become ready.
	StackProgressPending = StackProgress("Pending")
	// StackProgressDeploying is Stack's status when it's waiting for the Stack instance and its associated service
	// to become ready.
	StackProgressDeploying = StackProgress("Deploying")
	// StackProgressRunning is Stack's status when Stack is up and running.
	StackProgressRunning = StackProgress("Running")
)

type StackConditionType string

const (
	StackReady StackConditionType = "Ready"
)

type StackCondition struct {
	Type   StackConditionType     `json:"type"`
	Status corev1.ConditionStatus `json:"status"`

	// LastTransitionTime is the last time the condition transited from one status to another.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// StackStatus defines the observed state of Stack
type StackStatus struct {
	// Progress is the progress of this Stack object.
	// +kubebuilder:validation:Enum=Pending;Deploying;Configuring;Running
	// +optional
	Progress StackProgress `json:"progress,omitempty"`

	// +optional
	DeployedServices []string `json:"deployedServices,omitempty"`

	// +optional
	Conditions []StackCondition `json:"conditions,omitempty"`
}

func (in *StackStatus) GetConditionStatus(conditionType StackConditionType) corev1.ConditionStatus {
	if in != nil {
		for _, condition := range in.Conditions {
			if condition.Type == conditionType {
				return condition.Status
			}
		}
	}
	return corev1.ConditionUnknown
}

func (in *StackStatus) SetCondition(condition StackCondition) {
	for i, c := range in.Conditions {
		if c.Type == condition.Type {
			in.Conditions[i] = condition
			return
		}
	}
	in.Conditions = append(in.Conditions, condition)
}

func (in *StackStatus) IsReady() bool {
	return in != nil && in.GetConditionStatus(StackReady) == corev1.ConditionTrue
}

func (in *StackStatus) SetReady() {
	now := metav1.Now()
	in.SetCondition(StackCondition{
		Type:               StackReady,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: &now,
	})
}

func (in *StackStatus) SetNotReady() {
	now := metav1.Now()
	in.SetCondition(StackCondition{
		Type:               StackReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: &now,
	})
}

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

	Spec   StackSpec   `json:"spec,omitempty"`
	Status StackStatus `json:"status,omitempty"`
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
