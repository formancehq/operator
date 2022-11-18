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

	. "github.com/numary/operator/apis/sharedtypes"
	"github.com/numary/operator/internal/collectionutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type ElasticSearchTLSConfig struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	SkipCertVerify bool `json:"skipCertVerify,omitempty"`
}

type ElasticSearchBasicAuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ElasticSearchConfig struct {
	// +optional
	// +kubebuilder:validation:Enum:={http,https}
	// +kubebuilder:validation:default:=https
	Scheme string `json:"scheme,omitempty"`
	// +optional
	Host string `json:"host,omitempty"`
	// +optional
	HostFrom *ConfigSource `json:"hostFrom,omitempty"`
	// +optional
	Port uint16 `json:"port,omitempty"`
	// +optional
	PortFrom *ConfigSource `json:"portFrom,omitempty"`
	// +optional
	TLS ElasticSearchTLSConfig `json:"tls"`
	// +optional
	BasicAuth *ElasticSearchBasicAuthConfig `json:"basicAuth"`
}

func (in *ElasticSearchConfig) Endpoint() string {
	return fmt.Sprintf("%s://%s:%d", in.Scheme, in.Host, in.Port)
}

func (in *ElasticSearchConfig) Env(prefix string) []corev1.EnvVar {
	return []corev1.EnvVar{
		SelectRequiredConfigValueOrReference("OPEN_SEARCH_HOST", prefix, in.Host, in.HostFrom),
		SelectRequiredConfigValueOrReference("OPEN_SEARCH_PORT", prefix, in.Port, in.PortFrom),
		EnvWithPrefix(prefix, "OPEN_SEARCH_SCHEME", in.Scheme),
		EnvWithPrefix(prefix, "OPEN_SEARCH_SERVICE", ComputeEnvVar(prefix, "%s:%s",
			"OPEN_SEARCH_HOST",
			"OPEN_SEARCH_PORT",
		)),
	}
}

func (in *ElasticSearchConfig) Validate() field.ErrorList {
	return collectionutil.MergeAll(
		ValidateRequiredConfigValueOrReference("host", in.Host, in.HostFrom),
		ValidateRequiredConfigValueOrReference("port", in.Port, in.PortFrom),
	)
}

// SearchSpec defines the desired state of Search
type SearchSpec struct {
	Version  string `json:"version"`
	Scalable `json:",inline"`
	// +optional
	Ingress *IngressSpec `json:"ingress"`
	// +optional
	Debug bool `json:"debug"`
	// +optional
	Auth *AuthConfigSpec `json:"auth"`
	// +optional
	Monitoring    *MonitoringSpec     `json:"monitoring"`
	ElasticSearch ElasticSearchConfig `json:"elasticsearch"`
	KafkaConfig   KafkaConfig         `json:"kafka"`
	Index         string              `json:"index"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector

// Search is the Schema for the searches API
type Search struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SearchSpec        `json:"spec,omitempty"`
	Status ReplicationStatus `json:"status,omitempty"`
}

func (in *Search) GetStatus() Dirty {
	return &in.Status
}

func (in *Search) IsDirty(t Object) bool {
	return false
}

func (in *Search) GetConditions() *Conditions {
	return &in.Status.Conditions
}

//+kubebuilder:object:root=true

// SearchList contains a list of Search
type SearchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Search `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Search{}, &SearchList{})
}
