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

type RedisConfig struct {
	Uri string `json:"uri"`
	// +optional
	TLS bool `json:"tls"`
}

type PostgresConfigCreateDatabase struct {
	PostgresConfig `json:",inline"`
	CreateDatabase bool `json:"createDatabase"`
}

// LedgerSpec defines the desired state of Ledger
type LedgerSpec struct {
	// +optional
	Ingress *IngressSpec `json:"ingress"`
	// +optional
	Debug bool `json:"debug"`
	// +optional
	Redis    *RedisConfig                 `json:"redis"`
	Postgres PostgresConfigCreateDatabase `json:"postgres"`
	// +optional
	Auth *AuthConfigSpec `json:"auth"`
	// +optional
	Monitoring *MonitoringSpec `json:"monitoring"`
	// +optional
	Image string `json:"image"`
	// +optional
	Collector *CollectorConfigSpec `json:"collector"`
}

// LedgerStatus defines the observed state of Ledger
type LedgerStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Ledger is the Schema for the ledgers API
type Ledger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LedgerSpec   `json:"spec,omitempty"`
	Status LedgerStatus `json:"status,omitempty"`
}

func (a *Ledger) GetConditions() []Condition {
	return a.Status.Conditions
}

func (a *Ledger) setCondition(expectedCondition Condition) {
	for i, condition := range a.Status.Conditions {
		if condition.Type == expectedCondition.Type {
			a.Status.Conditions[i] = expectedCondition
			return
		}
	}
	a.Status.Conditions = append(a.Status.Conditions, expectedCondition)
}

func (a *Ledger) SetReady() {
	a.setCondition(Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetDeploymentCreated() {
	a.setCondition(Condition{
		Type:               ConditionTypeDeploymentCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetDeploymentFailure(err error) {
	a.setCondition(Condition{
		Type:               ConditionTypeDeploymentCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetServiceCreated() {
	a.setCondition(Condition{
		Type:               ConditionTypeServiceCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetServiceFailure(err error) {
	a.setCondition(Condition{
		Type:               ConditionTypeServiceCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetIngressCreated() {
	a.setCondition(Condition{
		Type:               ConditionTypeIngressCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetIngressFailure(err error) {
	a.setCondition(Condition{
		Type:               ConditionTypeIngressCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Ledger) RemoveIngressStatus() {
	in.Status.Conditions = Filter(in.Status.Conditions, func(c Condition) bool {
		return c.Type != ConditionTypeIngressCreated
	})
}

//+kubebuilder:object:root=true

// LedgerList contains a list of Ledger
type LedgerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Ledger `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Ledger{}, &LedgerList{})
}
