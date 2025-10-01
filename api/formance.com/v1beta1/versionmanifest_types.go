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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VersionManifestSpec defines the desired state of VersionManifest
type VersionManifestSpec struct {
	// Component name (e.g., "ledger", "payments")
	Component string `json:"component"`

	// Version range using semver (e.g., ">=v2.2.0 <v2.3.0")
	VersionRange string `json:"versionRange"`

	// Inherit from another manifest (optional)
	// +optional
	Extends string `json:"extends,omitempty"`

	// Environment variable prefix (e.g., "NUMARY_" or "")
	// +optional
	EnvVarPrefix string `json:"envVarPrefix,omitempty"`

	// Stream configurations
	// +optional
	Streams StreamsConfig `json:"streams,omitempty"`

	// Migration configuration
	// +optional
	Migration MigrationConfig `json:"migration,omitempty"`

	// Deployment architecture
	Architecture ArchitectureConfig `json:"architecture"`

	// Feature flags
	// +optional
	Features map[string]bool `json:"features,omitempty"`

	// Gateway configuration
	// +optional
	Gateway GatewayConfig `json:"gateway,omitempty"`

	// Authorization configuration (scopes, permissions)
	// +optional
	Authorization AuthorizationConfig `json:"authorization,omitempty"`
}

type StreamsConfig struct {
	// +optional
	Ingestion string `json:"ingestion,omitempty"`
	// +optional
	Reindex string `json:"reindex,omitempty"`
}

type MigrationConfig struct {
	Enabled bool `json:"enabled"`
	// Strategy: "strict", "continue-on-error", "skip"
	// +optional
	// +kubebuilder:default:="strict"
	Strategy string `json:"strategy,omitempty"`
	// +optional
	Commands []string `json:"commands,omitempty"`
	// +optional
	Conditions []MigrationCondition `json:"conditions,omitempty"`
}

type MigrationCondition struct {
	VersionRange string   `json:"versionRange"`
	Commands     []string `json:"commands"`
}

type ArchitectureConfig struct {
	// Type: "stateless", "single-or-multi-writer", "sharded"
	// +kubebuilder:validation:Enum=stateless;single-or-multi-writer;sharded
	Type string `json:"type"`

	Deployments []DeploymentSpec `json:"deployments"`

	// +optional
	Cleanup CleanupConfig `json:"cleanup,omitempty"`
}

type DeploymentSpec struct {
	Name string `json:"name"`

	// Replicas: "auto" or integer as string
	Replicas string `json:"replicas"`

	// +optional
	Stateful bool `json:"stateful,omitempty"`

	Containers []ContainerSpec `json:"containers"`

	// +optional
	Service *ServiceSpec `json:"service,omitempty"`
}

type ContainerSpec struct {
	Name string `json:"name"`

	// +optional
	Args []string `json:"args,omitempty"`

	// +optional
	Ports []PortSpec `json:"ports,omitempty"`

	// +optional
	HealthCheck *HealthCheckSpec `json:"healthCheck,omitempty"`

	// +optional
	Environment []EnvVar `json:"environment,omitempty"`

	// +optional
	ConditionalEnvironment []ConditionalEnv `json:"conditionalEnvironment,omitempty"`

	// +optional
	VolumeMounts []VolumeMount `json:"volumeMounts,omitempty"`
}

type PortSpec struct {
	Name string `json:"name"`
	Port int32  `json:"port"`
}

type HealthCheckSpec struct {
	Path string `json:"path"`
	// Type: "http", "tcp", "exec"
	// +kubebuilder:validation:Enum=http;tcp;exec
	Type string `json:"type"`
}

type EnvVar struct {
	Name string `json:"name"`

	// +optional
	Value string `json:"value,omitempty"`

	// +optional
	ValueFrom *EnvVarSource `json:"valueFrom,omitempty"`
}

type EnvVarSource struct {
	// +optional
	SettingKey string `json:"settingKey,omitempty"`
}

type ConditionalEnv struct {
	// Condition expression (e.g., "settings.ledger.experimental-features == true")
	When string `json:"when"`

	Env []EnvVar `json:"env"`
}

type VolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	// +optional
	ReadOnly bool `json:"readOnly,omitempty"`
}

type ServiceSpec struct {
	// Type: "ClusterIP", "NodePort", "LoadBalancer"
	// +optional
	// +kubebuilder:default:="ClusterIP"
	Type string `json:"type,omitempty"`

	Ports []PortSpec `json:"ports"`
}

type CleanupConfig struct {
	// +optional
	Deployments []string `json:"deployments,omitempty"`
	// +optional
	Services []string `json:"services,omitempty"`
	// +optional
	Reason string `json:"reason,omitempty"`
}

type GatewayConfig struct {
	Enabled bool `json:"enabled"`
	// +optional
	// +kubebuilder:default:="_healthcheck"
	HealthCheckEndpoint string `json:"healthCheckEndpoint,omitempty"`
}

type AuthorizationConfig struct {
	// OAuth/OIDC scopes available for this version
	// +optional
	Scopes []ScopeDefinition `json:"scopes,omitempty"`
}

type ScopeDefinition struct {
	// Scope name (e.g., "ledger:read", "ledger:write")
	Name string `json:"name"`

	// Human-readable description
	// +optional
	Description string `json:"description,omitempty"`

	// Whether this scope is deprecated
	// +optional
	Deprecated bool `json:"deprecated,omitempty"`

	// Replacement scope name if deprecated
	// +optional
	ReplacedBy string `json:"replacedBy,omitempty"`

	// Version when this scope was introduced
	// +optional
	Since string `json:"since,omitempty"`
}

// VersionManifestStatus defines the observed state of VersionManifest
type VersionManifestStatus struct {
	// +optional
	LastApplied *metav1.Time `json:"lastApplied,omitempty"`
}

// VersionManifest is the Schema for version manifests.
// It defines the deployment configuration for a specific version range of a component.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Component",type=string,JSONPath=".spec.component",description="Component name"
// +kubebuilder:printcolumn:name="Version Range",type=string,JSONPath=".spec.versionRange",description="Version range"
// +kubebuilder:printcolumn:name="Architecture",type=string,JSONPath=".spec.architecture.type",description="Architecture type"
type VersionManifest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VersionManifestSpec   `json:"spec,omitempty"`
	Status VersionManifestStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VersionManifestList contains a list of VersionManifest
type VersionManifestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VersionManifest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VersionManifest{}, &VersionManifestList{})
}
