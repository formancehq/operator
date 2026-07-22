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

type GatewayGRPCAPISpec struct {
	StackDependency `json:",inline"`
	// Name indicates the module name (e.g. "ledger")
	Name string `json:"name"`
	// GRPCServices is the list of fully-qualified gRPC service names
	// exposed by this module (e.g. "formance.ledger.v1.LedgerService")
	GRPCServices []string `json:"grpcServices"`
	// Port is the gRPC port on the backend service
	//+optional
	//+kubebuilder:default:=8081
	Port int32 `json:"port,omitempty"`
	// BackendRef overrides the historical <name>-grpc Service.
	// +optional
	BackendRef *GatewayBackendRef `json:"backendRef,omitempty"`
}

type GatewayGRPCAPIStatus struct {
	Status `json:",inline"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Stack",type=string,JSONPath=".spec.stack",description="Stack"
//+kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.ready",description="Ready"

// GatewayGRPCAPI is the Schema for the GRPCAPIs API
type GatewayGRPCAPI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayGRPCAPISpec   `json:"spec,omitempty"`
	Status GatewayGRPCAPIStatus `json:"status,omitempty"`
}

func (in *GatewayGRPCAPI) SetReady(b bool) {
	in.Status.Ready = b
}

func (in *GatewayGRPCAPI) IsReady() bool {
	return in.Status.Ready
}

func (in *GatewayGRPCAPI) SetError(s string) {
	in.Status.Info = s
}

func (a GatewayGRPCAPI) GetStack() string {
	return a.Spec.Stack
}

func (in *GatewayGRPCAPI) GetConditions() *Conditions {
	return &in.Status.Conditions
}

//+kubebuilder:object:root=true

// GatewayGRPCAPIList contains a list of GatewayGRPCAPI
type GatewayGRPCAPIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatewayGRPCAPI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GatewayGRPCAPI{}, &GatewayGRPCAPIList{})
}
