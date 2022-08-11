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
	"github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/collectionutil"
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
	Monitoring *sharedtypes.MonitoringSpec `json:"monitoring,omitempty"`
	// +optional
	Services ServicesSpec `json:"services,omitempty"`
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`
	// +optional
	Ingress *IngressStack `json:"ingress"`
	// +optional
	Collector *sharedtypes.CollectorConfigSpec `json:"collector"`
}

type AuthSpec struct {
	// +optional
	Image               string                                                 `json:"image"`
	PostgresConfig      sharedtypes.PostgresConfig                             `json:"postgres"`
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

type StackCondition struct {
	// type of condition in CamelCase or in foo.example.com/CamelCase.
	// ---
	// Many .condition.type values are consistent across resources like Available, but because arbitrary conditions can be
	// useful (see .node.status.conditions), the ability to deconflict is important.
	// The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`
	// status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// observedGeneration represents the .metadata.generation that the condition was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9, the condition is out of date
	// with respect to the current state of the instance.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`
	// lastTransitionTime is the last time the condition transitioned from one status to another.
	// This should be when the underlying condition changed.  If that is not known, then using the time when the API field changed is acceptable.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime" protobuf:"bytes,4,opt,name=lastTransitionTime"`
}

func (in StackCondition) GetType() string {
	return in.Type
}

func (in StackCondition) GetStatus() metav1.ConditionStatus {
	return in.Status
}

func (in StackCondition) GetObservedGeneration() int64 {
	return in.ObservedGeneration
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
	Conditions []StackCondition `json:"conditions,omitempty"`
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

func (in *Stack) GetConditions() []StackCondition {
	return in.Status.Conditions
}

func (in *Stack) Condition(conditionType string) *StackCondition {
	if in != nil {
		for _, condition := range in.Status.Conditions {
			if condition.Type == conditionType {
				return &condition
			}
		}
	}
	return nil
}

func (in *Stack) setCondition(condition StackCondition) {
	for i, c := range in.Status.Conditions {
		if c.Type == condition.Type {
			in.Status.Conditions[i] = condition
			return
		}
	}
	in.Status.Conditions = append(in.Status.Conditions, condition)
}

func (in *Stack) removeCondition(v string) {
	in.Status.Conditions = Filter(in.Status.Conditions, func(stack StackCondition) bool {
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
	in.setCondition(StackCondition{
		Type:               ConditionTypeStackProgressing,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
	in.setCondition(StackCondition{
		Type:               ConditionTypeStackReady,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
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
	in.setCondition(StackCondition{
		Type:               ConditionTypeStackReady,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) SetNamespaceCreated() {
	in.setCondition(StackCondition{
		Type:               ConditionTypeStackNamespaceCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) SetAuthCreated() {
	in.setCondition(StackCondition{
		Type:               ConditionTypeStackAuthCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) SetLedgerCreated() {
	in.setCondition(StackCondition{
		Type:               ConditionTypeStackLedgerCreated,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: in.Generation,
	})
}

func (in *Stack) RemoveAuthStatus() {
	in.removeCondition(ConditionTypeStackAuthCreated)
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
