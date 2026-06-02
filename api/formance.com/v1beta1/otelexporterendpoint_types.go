/*
Copyright 2023.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OtelAuthConfig struct {
	// +kubebuilder:validation:Enum=bearer
	Type       string `json:"type"`
	FromSecret string `json:"fromSecret"`
}

type OtelSignalConfig struct {
	Endpoint string `json:"endpoint"`

	// +optional
	Auth *OtelAuthConfig `json:"auth,omitempty"`
}

type OtelExporterEndpointSpec struct {
	// +optional
	StackSelector *metav1.LabelSelector `json:"stackSelector,omitempty"`

	// +optional
	Traces *OtelSignalConfig `json:"traces,omitempty"`

	// +optional
	Metrics *OtelSignalConfig `json:"metrics,omitempty"`

	// +optional
	ResourceAttributes map[string]string `json:"resourceAttributes,omitempty"`
}

type OtelExporterEndpointStatus struct {
	Status `json:",inline"`
	// +optional
	Stacks []string `json:"stacks,omitempty"`
}

// OtelExporterEndpoint configures an OpenTelemetry collector proxy for exporting traces and metrics.
// Multiple OtelExporterEndpoints can target the same stacks — the collector fans out to all matching destinations.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.ready",description="Is ready"
// +kubebuilder:printcolumn:name="Info",type=string,JSONPath=".status.info",description="Info"
type OtelExporterEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OtelExporterEndpointSpec   `json:"spec,omitempty"`
	Status OtelExporterEndpointStatus `json:"status,omitempty"`
}

func (in *OtelExporterEndpoint) IsReady() bool {
	return in.Status.Ready
}

func (in *OtelExporterEndpoint) SetReady(b bool) {
	in.Status.Ready = b
}

func (in *OtelExporterEndpoint) SetError(s string) {
	in.Status.Info = s
}

func (in *OtelExporterEndpoint) GetConditions() *Conditions {
	return &in.Status.Conditions
}

// +kubebuilder:object:root=true
type OtelExporterEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OtelExporterEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OtelExporterEndpoint{}, &OtelExporterEndpointList{})
}
