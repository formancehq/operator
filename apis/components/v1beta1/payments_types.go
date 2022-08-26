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

	. "github.com/numary/formance-operator/apis/sharedtypes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MongoDBConfig struct {
	// +required
	Host string `json:"host"`
	// +required
	Port uint16 `json:"port"`
	// +required
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	UseSrv   bool   `json:"useSrv"`
}

func (cfg MongoDBConfig) Uri() string {
	var credentialsPart string
	if cfg.Username != "" {
		credentialsPart = fmt.Sprintf("%s:%s@", cfg.Username, cfg.Password)
	}
	var portPart string
	scheme := "mongodb"
	if cfg.UseSrv {
		scheme = scheme + "+srv"
	} else {
		portPart = fmt.Sprintf(":%d", cfg.Port)
	}
	return fmt.Sprintf("%s://%s%s%s", scheme, credentialsPart, cfg.Host, portPart)
}

// PaymentsSpec defines the desired state of Payments
type PaymentsSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Ingress *IngressSpec `json:"ingress"`
	// +optional
	Debug bool `json:"debug"`
	// +optional
	Auth *AuthConfigSpec `json:"auth"`
	// +optional
	Monitoring *MonitoringSpec `json:"monitoring"`
	// +optional
	Collector          *CollectorConfig `json:"collector"`
	ElasticSearchIndex string           `json:"elasticSearchIndex"`

	MongoDB MongoDBConfig `json:"mongoDB"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Payments is the Schema for the payments API
type Payments struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PaymentsSpec `json:"spec,omitempty"`
	Status Status       `json:"status,omitempty"`
}

func (in *Payments) GetStatus() Dirty {
	return &in.Status
}

func (in *Payments) GetConditions() *Conditions {
	return &in.Status.Conditions
}

func (in *Payments) IsDirty(t Object) bool {
	return false
}

//+kubebuilder:object:root=true

// PaymentsList contains a list of Payments
type PaymentsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Payments `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Payments{}, &PaymentsList{})
}
