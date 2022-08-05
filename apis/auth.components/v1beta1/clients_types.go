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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClientSpec defines the desired state of Client
type ClientSpec struct {
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

// ClientStatus defines the observed state of Client
type ClientStatus struct {
	Conditions   []metav1.Condition `json:"conditions"`
	Ready        bool               `json:"ready"`
	AuthServerID string             `json:"authServerID,omitempty"`
	// +optional
	Scopes map[string]string `json:"scopes"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Client is the Schema for the oauths API
type Client struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClientSpec   `json:"spec,omitempty"`
	Status ClientStatus `json:"status,omitempty"`
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

func (in *Client) setCondition(c metav1.Condition) {
	for ind, condition := range in.Status.Conditions {
		if condition.Type == c.Type {
			in.Status.Conditions[ind] = c
			return
		}
	}
	in.Status.Conditions = append(in.Status.Conditions, c)
}

func (in *Client) Progressing() {
	in.setCondition(metav1.Condition{
		Type:               ConditionTypeClientProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "SynchronizationStarted",
	})
	in.Status.Ready = false
}

func (in *Client) Ready() {
	in.setCondition(metav1.Condition{
		Type:               ConditionTypeClientProgressing,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "SynchronizationTerminated",
	})
	in.Status.Ready = true
}

func (in *Client) SetClientCreated(id string) {
	in.setCondition(metav1.Condition{
		Type:               ConditionTypeClientCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ClientCreated",
	})
	in.Status.AuthServerID = id
}

func (in *Client) SetClientUpdated() {
	in.setCondition(metav1.Condition{
		Type:               ConditionTypeClientUpdated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ClientUpdated",
	})
}

func (in *Client) checkScopesSynchronized() {

	notSynchronized := func() {
		in.setCondition(metav1.Condition{
			Type:               ConditionTypeScopesSynchronized,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "ScopesNotSynchronized",
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
	in.setCondition(metav1.Condition{
		Type:               ConditionTypeScopesSynchronized,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ScopesSynchronized",
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
