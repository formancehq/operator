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
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	"github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/pkg/collectionutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IngressStack struct {
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
	Monitoring *sharedtypes.MonitoringSpec `json:"monitoring,omitempty"`
	// +optional
	Services ServicesSpec `json:"services,omitempty"`
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`
	// +optional
	Ingress *IngressStack `json:"ingress"`
}

type AuthSpec struct {
	// +optional
	Image               string                                                 `json:"image"`
	PostgresConfig      authcomponentsv1beta1.PostgresConfig                   `json:"postgres"`
	SigningKey          string                                                 `json:"signingKey"`
	DelegatedOIDCServer authcomponentsv1beta1.DelegatedOIDCServerConfiguration `json:"delegatedOIDCServer"`
}

type ServicesSpec struct {
	Control  ControlSpec  `json:"control,omitempty"`
	Ledger   LedgerSpec   `json:"ledger,omitempty"`
	Payments PaymentsSpec `json:"payments,omitempty"`
	Search   SearchSpec   `json:"search,omitempty"`
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

type ConditionType string

const (
	ConditionTypeStackProgressing      ConditionType = "Progressing"
	ConditionTypeStackReady            ConditionType = "Ready"
	ConditionTypeStackNamespaceCreated ConditionType = "NamespaceCreated"
	ConditionTypeStackAuthCreated      ConditionType = "AuthCreated"
)

type ConditionStack struct {
	Type   ConditionType          `json:"type"`
	Status metav1.ConditionStatus `json:"status"`

	// LastTransitionTime is the last time the condition transited from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// StackStatus defines the observed state of Stack
type StackStatus struct {
	// Progress is the progress of this Stack object.
	// +kubebuilder:validation:Enum=Pending;Deploying;Configuring;Running
	// +optional
	Progress StackProgress `json:"progress,omitempty"`

	// +optional
	DeployedServices []string `json:"deployedServices,omitempty"`

	// +optional
	Conditions []ConditionStack `json:"conditions,omitempty"`
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

func (in *Stack) Condition(conditionType ConditionType) *ConditionStack {
	if in != nil {
		for _, condition := range in.Status.Conditions {
			if condition.Type == conditionType {
				return &condition
			}
		}
	}
	return nil
}

func (in *Stack) setCondition(condition ConditionStack) {
	for i, c := range in.Status.Conditions {
		if c.Type == condition.Type {
			in.Status.Conditions[i] = condition
			return
		}
	}
	in.Status.Conditions = append(in.Status.Conditions, condition)
}

func (in *Stack) removeCondition(v ConditionType) {
	in.Status.Conditions = Filter(in.Status.Conditions, func(stack ConditionStack) bool {
		return stack.Type != v
	})
}

func (in *Stack) Scheme() string {
	if in.Spec.Scheme != "" {
		return in.Spec.Scheme
	}
	return "https"
}

func (in *Stack) Progress() {
	in.setCondition(ConditionStack{
		Type:               ConditionTypeStackProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	})
	in.setCondition(ConditionStack{
		Type:               ConditionTypeStackReady,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Stack) IsReady() bool {
	condition := in.Condition(ConditionTypeStackReady)
	if condition == nil {
		return false
	}
	return in != nil && condition.Status == metav1.ConditionTrue
}

func (in *Stack) SetReady() {
	in.removeCondition(ConditionTypeStackProgressing)
	in.setCondition(ConditionStack{
		Type:               ConditionTypeStackReady,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Stack) SetNamespaceCreated() {
	in.setCondition(ConditionStack{
		Type:               ConditionTypeStackNamespaceCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Stack) SetAuthCreated() {
	in.setCondition(ConditionStack{
		Type:               ConditionTypeStackAuthCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	})
}

func (in *Stack) RemoveAuthStatus() {
	in.removeCondition(ConditionTypeStackAuthCreated)
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
