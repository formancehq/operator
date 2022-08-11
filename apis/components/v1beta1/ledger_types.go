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
	"github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type LedgerCondition struct {
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

func (in LedgerCondition) GetType() string {
	return in.Type
}

func (in LedgerCondition) GetStatus() metav1.ConditionStatus {
	return in.Status
}

func (in LedgerCondition) GetObservedGeneration() int64 {
	return in.ObservedGeneration
}

type RedisConfig struct {
	Uri string `json:"uri"`
	// +optional
	TLS bool `json:"tls"`
}

type PostgresConfig struct {
	sharedtypes.PostgresConfig `json:",inline"`
	CreateDatabase             bool `json:"createDatabase"`
}

// LedgerSpec defines the desired state of Ledger
type LedgerSpec struct {
	// +optional
	Ingress *sharedtypes.IngressSpec `json:"ingress"`
	// +optional
	Debug bool `json:"debug"`
	// +optional
	Redis    *RedisConfig   `json:"redis"`
	Postgres PostgresConfig `json:"postgres"`
	// +optional
	Auth *sharedtypes.AuthConfigSpec `json:"auth"`
	// +optional
	Monitoring *sharedtypes.MonitoringSpec `json:"monitoring"`
	// +optional
	Image string `json:"image"`
	// +optional
	Collector *sharedtypes.CollectorConfigSpec `json:"collector"`
}

// LedgerStatus defines the observed state of Ledger
type LedgerStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []LedgerCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
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

func (a *Ledger) GetConditions() []LedgerCondition {
	return a.Status.Conditions
}

func (a *Ledger) setCondition(expectedCondition LedgerCondition) {
	for i, condition := range a.Status.Conditions {
		if condition.Type == expectedCondition.Type {
			a.Status.Conditions[i] = expectedCondition
			return
		}
	}
	a.Status.Conditions = append(a.Status.Conditions, expectedCondition)
}

func (a *Ledger) SetReady() {
	a.setCondition(LedgerCondition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetDeploymentCreated() {
	a.setCondition(LedgerCondition{
		Type:               ConditionTypeDeploymentCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetDeploymentFailure(err error) {
	a.setCondition(LedgerCondition{
		Type:               ConditionTypeDeploymentCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetServiceCreated() {
	a.setCondition(LedgerCondition{
		Type:               ConditionTypeServiceCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetServiceFailure(err error) {
	a.setCondition(LedgerCondition{
		Type:               ConditionTypeServiceCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetIngressCreated() {
	a.setCondition(LedgerCondition{
		Type:               ConditionTypeIngressCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Ledger) SetIngressFailure(err error) {
	a.setCondition(LedgerCondition{
		Type:               ConditionTypeIngressCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Ledger) RemoveIngressStatus() {
	in.Status.Conditions = Filter(in.Status.Conditions, func(c LedgerCondition) bool {
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
