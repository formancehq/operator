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
	"fmt"

	"github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DelegatedOIDCServerConfiguration struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret"`
}

type PostgresConfig struct {
	Database string `json:"database"`
	Port     int    `json:"port"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c PostgresConfig) URI() string {
	return fmt.Sprintf(
		"postgresql://%s:%s@%s:%d/%s",
		c.Username,
		c.Password,
		c.Host,
		c.Port,
		c.Database,
	)
}

type IngressSpec struct {
	Path string `json:"path"`
	Host string `json:"host"`
	// +optional
	Annotations map[string]string `json:"annotations"`
}

// AuthSpec defines the desired state of Auth
type AuthSpec struct {
	// +kubebuilder:validation:Optional
	Image      string         `json:"image,omitempty"`
	Postgres   PostgresConfig `json:"postgres"`
	BaseURL    string         `json:"baseURL"`
	SigningKey string         `json:"signingKey"`
	DevMode    bool           `json:"devMode"`
	// +optional
	Ingress *IngressSpec `json:"ingress"`

	DelegatedOIDCServer DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`

	// +optional
	Monitoring *sharedtypes.MonitoringSpec `json:"monitoring"`
}

const (
	ConditionTypeDeploymentCreated = "DeploymentCreated"
	ConditionTypeServiceCreated    = "ServiceCreated"
	ConditionTypeIngressCreated    = "IngressCreated"
	ConditionTypeReady             = "Ready"
)

type AuthCondition struct {
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

func (c AuthCondition) GetObservedGeneration() int64 {
	return c.ObservedGeneration
}

func (c AuthCondition) GetType() string {
	return c.Type
}

func (c AuthCondition) GetStatus() metav1.ConditionStatus {
	return c.Status
}

// AuthStatus defines the observed state of Auth
type AuthStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []AuthCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
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

func (a *Auth) GetConditions() []AuthCondition {
	return a.Status.Conditions
}

func (a *Auth) setCondition(expectedCondition AuthCondition) {
	for i, condition := range a.Status.Conditions {
		if condition.Type == expectedCondition.Type {
			a.Status.Conditions[i] = expectedCondition
			return
		}
	}
	a.Status.Conditions = append(a.Status.Conditions, expectedCondition)
}

func (a *Auth) SetReady() {
	a.setCondition(AuthCondition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetDeploymentCreated() {
	a.setCondition(AuthCondition{
		Type:               ConditionTypeDeploymentCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetDeploymentFailure(err error) {
	a.setCondition(AuthCondition{
		Type:               ConditionTypeDeploymentCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetServiceCreated() {
	a.setCondition(AuthCondition{
		Type:               ConditionTypeServiceCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetServiceFailure(err error) {
	a.setCondition(AuthCondition{
		Type:               ConditionTypeServiceCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetIngressCreated() {
	a.setCondition(AuthCondition{
		Type:               ConditionTypeIngressCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (a *Auth) SetIngressFailure(err error) {
	a.setCondition(AuthCondition{
		Type:               ConditionTypeIngressCreated,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: a.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Auth) RemoveIngressStatus() {
	in.Status.Conditions = Filter(in.Status.Conditions, func(c AuthCondition) bool {
		return c.Type != ConditionTypeIngressCreated
	})
}

func (in *Auth) Condition(conditionType string) *AuthCondition {
	return First(in.Status.Conditions, func(c AuthCondition) bool {
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
