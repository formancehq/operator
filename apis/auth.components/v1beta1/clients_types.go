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
	Name string `json:"name"`
	// +optional
	Description *string `json:"description,omitempty"`
	// +optional
	RedirectUris []string `json:"redirectUris,omitempty"`
	// +optional
	PostLogoutRedirectUris []string `json:"postLogoutRedirectUris"`
	// +optional
	Scopes []string `json:"scopes"`
}

// ClientStatus defines the observed state of Client
type ClientStatus struct {
	Synchronized bool   `json:"synchronized"`
	Error        string `json:"error,omitempty"`
	AuthServerID string `json:"authServerID,omitempty"`
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
	if len(client.RedirectUris) != len(in.Spec.RedirectUris) {
		return false
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

func (in *Client) SetSynchronized() {
	in.Status.Error = ""
	in.Status.Synchronized = true
}

func (in *Client) SetSynchronizationError(err error) {
	in.Status.Synchronized = false
	in.Status.Error = err.Error()
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
