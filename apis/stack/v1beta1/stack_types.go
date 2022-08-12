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

	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IngressStack struct {
	// +optional
	Annotations map[string]string `json:"annotations"`
}

// StackSpec defines the desired state of Stack
type StackSpec struct {
	// +optional
	Debug bool `json:"debug"`
	// +required
	Version string `json:"version,omitempty"`
	// +required
	Namespace string `json:"namespace,omitempty"`
	// +required
	Host string `json:"host,omitempty"`
	// +optional
	Scheme string `json:"scheme,omitempty"`
	// +optional
	Monitoring *MonitoringSpec `json:"monitoring,omitempty"`
	// +optional
	Services ServicesSpec `json:"services,omitempty"`
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`
	// +optional
	Ingress *IngressStack `json:"ingress"`
	// +optional
	Collector *CollectorConfigSpec `json:"collector"`
}

type AuthSpec struct {
	// +optional
	Image               string                                                 `json:"image"`
	PostgresConfig      PostgresConfig                                         `json:"postgres"`
	SigningKey          string                                                 `json:"signingKey"`
	DelegatedOIDCServer authcomponentsv1beta1.DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`
}

type ServicesSpec struct {
	Control  *ControlSpec  `json:"control,omitempty"`
	Ledger   *LedgerSpec   `json:"ledger,omitempty"`
	Payments *PaymentsSpec `json:"payments,omitempty"`
	Search   *SearchSpec   `json:"search,omitempty"`
}

// StackProgress is a word summarizing the state of a Stack resource.
type StackProgress string

const (
	// StackProgressPending is Stack's status when it's waiting for the datacenter to become ready.
	StackProgressPending = StackProgress("Pending")
	// StackProgressDeploying is Stack's status when it's waiting for the Stack instance and its associated service
	// to become ready.
	StackProgressDeploying = StackProgress("Deploying")
	// StackProgressRunning is Stack's status when Stack is up and running.
	StackProgressRunning = StackProgress("Running")
)

const (
	ConditionTypeStackProgressing      = "Progressing"
	ConditionTypeStackReady            = "Ready"
	ConditionTypeStackNamespaceCreated = "NamespaceCreated"
	ConditionTypeStackAuthCreated      = "AuthCreated"
	ConditionTypeStackLedgerCreated    = "LedgerCreated"
)

// StackStatus defines the observed state of Stack
type StackStatus struct {
	Status `json:",inline"`

	// +optional
	DeployedServices []string `json:"deployedServices,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.progress`
//+kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="Stack Version"
//+kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".spec.namespace",description="Stack Namespace"

// Stack is the Schema for the stacks API
type Stack struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StackSpec   `json:"spec,omitempty"`
	Status StackStatus `json:"status,omitempty"`
}

func (in *Stack) GetConditions() []Condition {
	return in.Status.Conditions
}

func (in *Stack) Scheme() string {
	if in.Spec.Scheme != "" {
		return in.Spec.Scheme
	}
	return "https"
}

func (in *Stack) Progress() {
	in.Status.SetCondition(Condition{
		Type:               ConditionTypeStackProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.Status.SetCondition(Condition{
		Type:               ConditionTypeStackReady,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) IsReady() bool {
	condition := in.Status.GetCondition(ConditionTypeStackReady)
	if condition == nil {
		return false
	}
	return in != nil && condition.Status == metav1.ConditionTrue
}

func (in *Stack) SetReady() {
	in.Status.RemoveCondition(ConditionTypeStackProgressing)
	in.Status.SetCondition(Condition{
		Type:               ConditionTypeStackReady,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) SetNamespaceCreated() {
	in.Status.SetCondition(Condition{
		Type:               ConditionTypeStackNamespaceCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) SetAuthCreated() {
	in.Status.SetCondition(Condition{
		Type:               ConditionTypeStackAuthCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) SetLedgerCreated() {
	in.Status.SetCondition(Condition{
		Type:               ConditionTypeStackLedgerCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) RemoveAuthStatus() {
	in.Status.RemoveCondition(ConditionTypeStackAuthCreated)
}

func (s *Stack) ServiceName(v string) string {
	return fmt.Sprintf("%s-%s", s.Name, v)
}

//+kubebuilder:object:root=true

// StackList contains a list of Stack
type StackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Stack `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Stack{}, &StackList{})
}
