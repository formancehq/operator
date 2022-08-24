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
	"github.com/numary/formance-operator/internal/envutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RedisConfig struct {
	Uri string `json:"uri"`
	// +optional
	TLS bool `json:"tls"`
}

type PostgresConfigCreateDatabase struct {
	PostgresConfigWithDatabase `json:",inline"`
	CreateDatabase             bool `json:"createDatabase"`
}

type CollectorConfig struct {
	KafkaConfig `json:",inline"`
	Topic       string `json:"topic"`
}

func (c CollectorConfig) Env(prefix string) []corev1.EnvVar {
	ret := c.KafkaConfig.Env(prefix)
	return append(ret, envutil.EnvWithPrefix(prefix, "PUBLISHER_TOPIC_MAPPING", "*:"+c.Topic))
}

// LedgerSpec defines the desired state of Ledger
type LedgerSpec struct {
	ImageHolder `json:",inline"`
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
	Collector          *CollectorConfig `json:"collector"`
	ElasticSearchIndex string           `json:"elasticSearchIndex"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Ledger is the Schema for the ledgers API
type Ledger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LedgerSpec `json:"spec,omitempty"`
	Status Status     `json:"status,omitempty"`
}

func (a *Ledger) IsDirty(t Object) bool {
	return a.Status.IsDirty(t)
}

func (a *Ledger) GetConditions() *Conditions {
	return &a.Status.Conditions
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
