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

type DelegatedOIDCServerConfiguration struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret"`
}

// AuthSpec defines the desired state of Auth
type AuthSpec struct {
	// +kubebuilder:validation:Optional
	Image      string                       `json:"image,omitempty"`
	Postgres   PostgresConfigCreateDatabase `json:"postgres"`
	BaseURL    string                       `json:"baseURL"`
	SigningKey string                       `json:"signingKey"`
	DevMode    bool                         `json:"devMode"`
	// +optional
	Ingress *IngressSpec `json:"ingress"`

	DelegatedOIDCServer DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`

	// +optional
	Monitoring *MonitoringSpec `json:"monitoring"`
}

const (
	ConditionTypeDeploymentCreated = "DeploymentCreated"
	ConditionTypeServiceCreated    = "ServiceCreated"
	ConditionTypeIngressCreated    = "IngressCreated"
	ConditionTypeReady             = "Ready"
)

// AuthStatus defines the observed state of Auth
type AuthStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Auth is the Schema for the auths API
type Auth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuthSpec   `json:"spec,omitempty"`
	Status AuthStatus `json:"status,omitempty"`
}

func (a *Auth) GetConditions() []Condition {
	return a.Status.Conditions
}

func (a *Auth) setCondition(expectedCondition Condition) {
	for i, condition := range a.Status.Conditions {
		if condition.Type == expectedCondition.Type {
			a.Status.Conditions[i] = expectedCondition
			return
		}
	}
	a.Status.Conditions = append(a.Status.Conditions, expectedCondition)
}

func (a *Auth) SetReady() {
	a.setCondition(Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetDeploymentCreated() {
	a.setCondition(Condition{
		Type:               ConditionTypeDeploymentCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetDeploymentFailure(err error) {
	a.setCondition(Condition{
		Type:               ConditionTypeDeploymentCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetServiceCreated() {
	a.setCondition(Condition{
		Type:               ConditionTypeServiceCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetServiceFailure(err error) {
	a.setCondition(Condition{
		Type:               ConditionTypeServiceCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetIngressCreated() {
	a.setCondition(Condition{
		Type:               ConditionTypeIngressCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetIngressFailure(err error) {
	a.setCondition(Condition{
		Type:               ConditionTypeIngressCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Auth) RemoveIngressStatus() {
	in.Status.Conditions = Filter(in.Status.Conditions, func(c Condition) bool {
		return c.Type != ConditionTypeIngressCreated
	})
}

func (in *Auth) Condition(conditionType string) *Condition {
	return First(in.Status.Conditions, func(c Condition) bool {
		return c.Type == conditionType
	})
}

//+kubebuilder:object:root=true

// AuthList contains a list of Auth
type AuthList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Auth `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Auth{}, &AuthList{})
}
