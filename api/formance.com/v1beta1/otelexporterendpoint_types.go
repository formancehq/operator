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

// OtelExporterAuth configures per-signal authentication.
// Auth is per-signal so traces and metrics can use different credentials if needed.
type OtelExporterAuth struct {
	// Type is the authentication type.
	// +kubebuilder:validation:Enum=bearer
	Type string `json:"type"`
	// FromSecret references a Secret name.
	// The controller creates a ResourceReference to replicate the secret into each target stack namespace.
	// The source secret must have a "formance.com/stack" label set to "any" or a specific stack name.
	// +kubebuilder:validation:MinLength=1
	FromSecret string `json:"fromSecret"`
	// FromSecretKey is the key within the Secret that contains the token. Defaults to "token".
	// +optional
	// +kubebuilder:default="token"
	FromSecretKey string `json:"fromSecretKey,omitempty"`
}

// OtelSignalConfig configures a single signal type (traces or metrics).
// Each signal type has its own endpoint and authentication block, allowing
// different destinations or credentials per signal.
// Protocol is inferred from the URL scheme: grpc:// for gRPC, http:// or https:// for HTTP/protobuf (default).
type OtelSignalConfig struct {
	// Endpoint URL for the signal (e.g., "http://my-collector:4318", "grpc://my-collector:4317").
	// Supported schemes: http, https, grpc.
	// Protocol is inferred from the URL scheme. HTTP/protobuf is the default for firewall compatibility.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^(https?://|grpc://)`
	Endpoint string `json:"endpoint"`

	// Auth is the optional per-signal authentication configuration.
	// +optional
	Auth *OtelExporterAuth `json:"auth,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="has(self.traces) || has(self.metrics)",message="at least one signal (traces or metrics) must be configured"
type OtelExporterEndpointSpec struct {
	// StackSelector is a standard Kubernetes LabelSelector (matchLabels/matchExpressions).
	// One CRD can target all current and future stacks with a single selector.
	// Matches the pattern established by Settings.
	StackSelector *metav1.LabelSelector `json:"stackSelector"`

	// Traces configures the traces signal. At least one of traces or metrics must be set.
	// Logs are intentionally out of scope.
	// +optional
	Traces *OtelSignalConfig `json:"traces,omitempty"`

	// Metrics configures the metrics signal. At least one of traces or metrics must be set.
	// Logs are intentionally out of scope.
	// +optional
	Metrics *OtelSignalConfig `json:"metrics,omitempty"`

	// ResourceAttributes are injected into outgoing telemetry via a collector processor.
	// +optional
	ResourceAttributes map[string]string `json:"resourceAttributes,omitempty"`
}

// OtelExporterEndpointStatus represents the observed state of an OtelExporterEndpoint.
type OtelExporterEndpointStatus struct {
	Status `json:",inline"`
	// Stacks is a sorted list of stack names currently targeted by this endpoint.
	// Includes stacks with successful reconciliation and stacks with transient errors or pending cleanup.
	// Used by the finalizer to find previously matched stacks during deletion.
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
