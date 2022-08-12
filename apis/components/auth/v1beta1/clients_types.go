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
	"reflect"
	"sort"

	"github.com/numary/auth/authclient"
	. "github.com/numary/formance-operator/pkg/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClientSpec defines the desired state of Client
type ClientSpec struct {
	AuthServerReference string `json:"authServerReference"`
	// +optional
	Public bool `json:"public"`
	// +optional
	Description *string `json:"description,omitempty"`
	// +optional
	RedirectUris []string `json:"redirectUris"`
	// +optional
	PostLogoutRedirectUris []string `json:"postLogoutRedirectUris"`
	// +optional
	Scopes []string `json:"scopes"`
}

const (
	ConditionTypeClientProgressing  = "Progressing"
	ConditionTypeClientCreated      = "ClientCreated"
	ConditionTypeClientUpdated      = "ClientUpdated"
	ConditionTypeScopesSynchronized = "ScopesSynchronized"
)

type ClientCondition struct {
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

func (in ClientCondition) GetType() string {
	return in.Type
}

func (in ClientCondition) GetStatus() metav1.ConditionStatus {
	return in.Status
}

func (in ClientCondition) GetObservedGeneration() int64 {
	return in.ObservedGeneration
}

// ClientStatus defines the observed state of Client
type ClientStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions   []ClientCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	Ready        bool              `json:"ready"`
	AuthServerID string            `json:"authServerID,omitempty"`
	// +optional
	Scopes map[string]string `json:"scopes"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Server ID",type="string",JSONPath=".status.authServerID",description="Auth server ID"

// Client is the Schema for the oauths API
type Client struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClientSpec   `json:"spec,omitempty"`
	Status ClientStatus `json:"status,omitempty"`
}

func (in *Client) GetConditions() []ClientCondition {
	return in.Status.Conditions
}

func (in *Client) Condition(v string) *ClientCondition {
	return First(in.Status.Conditions, func(c ClientCondition) bool {
		return c.Type == v
	})
}

func (in *Client) AuthServerReference() string {
	return in.Spec.AuthServerReference
}

func (in *Client) IsCreatedOnAuthServer() bool {
	return in.Status.AuthServerID != ""
}

func (in *Client) ClearAuthServerID() {
	in.Status.AuthServerID = ""
}

func (in *Client) Match(client *authclient.Client) bool {
	if client.Name != in.Name {
		return false
	}
	if client.Description == nil && in.Spec.Description != nil {
		return false
	}
	if client.Description != nil && in.Spec.Description == nil {
		return false
	}
	if client.Description != nil && in.Spec.Description != nil {
		if *client.Description != *in.Spec.Description {
			return false
		}
	}

	sort.Strings(client.RedirectUris)
	sort.Strings(in.Spec.RedirectUris)
	if !reflect.DeepEqual(client.RedirectUris, in.Spec.RedirectUris) {
		return false
	}

	sort.Strings(client.PostLogoutRedirectUris)
	sort.Strings(in.Spec.PostLogoutRedirectUris)
	if !reflect.DeepEqual(client.PostLogoutRedirectUris, in.Spec.PostLogoutRedirectUris) {
		return false
	}

	if in.Spec.Public && (client.Public == nil || !*client.Public) {
		return false
	}
	if !in.Spec.Public && client.Public != nil && *client.Public {
		return false
	}

	return true
}

func (in *Client) setCondition(c ClientCondition) {
	for ind, condition := range in.Status.Conditions {
		if condition.Type == c.Type {
			in.Status.Conditions[ind] = c
			return
		}
	}
	in.Status.Conditions = append(in.Status.Conditions, c)
}

func (in *Client) Progress() {
	in.setCondition(ClientCondition{
		Type:               ConditionTypeClientProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.Status.Ready = false
}

func (in *Client) StopProgression() {
	in.setCondition(ClientCondition{
		Type:               ConditionTypeClientProgressing,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.Status.Ready = true
}

func (in *Client) SetClientCreated(id string) {
	in.setCondition(ClientCondition{
		Type:               ConditionTypeClientCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.Status.AuthServerID = id
}

func (in *Client) SetClientUpdated() {
	in.setCondition(ClientCondition{
		Type:               ConditionTypeClientUpdated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Client) checkScopesSynchronized() {

	notSynchronized := func() {
		in.setCondition(ClientCondition{
			Type:               ConditionTypeScopesSynchronized,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: in.Generation,
		})
	}

	if len(in.Spec.Scopes) != len(in.Status.Scopes) {
		notSynchronized()
		return
	}
	for _, wantedScope := range in.Spec.Scopes {
		if _, ok := in.Status.Scopes[wantedScope]; !ok {
			notSynchronized()
			return
		}
	}
	// Scopes synchronized
	in.setCondition(ClientCondition{
		Type:               ConditionTypeScopesSynchronized,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Client) SetScopeSynchronized(scope *Scope) {
	_, ok := in.Status.Scopes[scope.Name]
	if ok {
		return
	}
	if in.Status.Scopes == nil {
		in.Status.Scopes = map[string]string{}
	}
	in.Status.Scopes[scope.Name] = scope.Status.AuthServerID
	in.checkScopesSynchronized()
}

func (in *Client) SetScopesRemoved(authServerID string) {
	for name, scopeAuthServerId := range in.Status.Scopes {
		if scopeAuthServerId == authServerID {
			delete(in.Status.Scopes, name)
			return
		}
	}
	in.checkScopesSynchronized()
}

func (in *Client) AddScopeSpec(scope *Scope) {
	in.Spec.Scopes = append(in.Spec.Scopes, scope.Name)
}

func NewClient(name string) *Client {
	return &Client{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

//+kubebuilder:object:root=true

// ClientList contains a list of Client
type ClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Client `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Client{}, &ClientList{})
}
