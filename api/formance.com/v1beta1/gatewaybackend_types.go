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

const GatewayBackendTLSSecretLabel = "formance.com/gateway-backend-tls"

// GatewayBackendTLS configures TLS when Gateway connects to a backend.
type GatewayBackendTLS struct {
	// SecretName contains the CA used to verify the backend certificate.
	// The Secret must carry the `formance.com/gateway-backend-tls: "true"`
	// label so that certificate rotations trigger a Gateway rollout.
	// +kubebuilder:validation:MinLength=1
	SecretName string `json:"secretName"`
	// CASecretKey is the key containing the CA certificate.
	// +optional
	// +kubebuilder:default:="ca.crt"
	CASecretKey string `json:"caSecretKey,omitempty"`
	// ServerName is used for backend certificate verification.
	// +kubebuilder:validation:MinLength=1
	ServerName string `json:"serverName"`
}

// GatewayBackendRef selects the Kubernetes Service used by a Gateway route.
// When omitted, Gateway keeps using the module's historical Service.
type GatewayBackendRef struct {
	// Name is the backend Service name in the Stack namespace.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Port is the backend Service port.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
	// TLS enables a verified TLS connection to the backend.
	// +optional
	TLS *GatewayBackendTLS `json:"tls,omitempty"`
}
