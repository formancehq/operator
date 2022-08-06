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

	"github.com/numary/formance-operator/apis/stack/v1beta1"
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
	return fmt.Sprintf("postgresql://%s:%s@%s:%d/%s",
		c.Username,
		c.Password,
		c.Host,
		c.Port,
		c.Database,
	)
}

// AuthSpec defines the desired state of Auth
type AuthSpec struct {
	// +kubebuilder:validation:Optional
	Image      string         `json:"image,omitempty"`
	Postgres   PostgresConfig `json:"postgres"`
	BaseURL    string         `json:"baseURL"`
	SigningKey string         `json:"signingKey"`
	DevMode    bool           `json:"devMode"`

	DelegatedOIDCServer DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`

	// +optional
	Monitoring *v1beta1.MonitoringSpec `json:"monitoring"`
}

// AuthStatus defines the observed state of Auth
type AuthStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
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
