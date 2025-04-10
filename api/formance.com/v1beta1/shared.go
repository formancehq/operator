package v1beta1

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"

	"github.com/formancehq/go-libs/v2/pointer"
	"golang.org/x/mod/semver"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:generate=false
type EventPublisher interface {
	isEventPublisher()
}

type DevProperties struct {
	// +optional
	// Allow to enable debug mode on the module
	// +kubebuilder:default:=false
	Debug bool `json:"debug"`
	// +optional
	// Allow to enable dev mode on the module
	// Dev mode is used to allow some application to do custom setup in development mode (allow insecure certificates for example)
	// +kubebuilder:default:=false
	Dev bool `json:"dev"`
}

func (p DevProperties) IsDebug() bool {
	return p.Debug
}

func (p DevProperties) IsDev() bool {
	return p.Dev
}

// Condition contains details for one aspect of the current state of this API Resource.
// ---
// This struct is intended for direct use as an array at the field path .status.conditions.  For example,
//
//	type FooStatus struct{
//	    // Represents the observations of a foo's current state.
//	    // Known .status.conditions.type are: "Available", "Progressing", and "Degraded"
//	    // +patchMergeKey=type
//	    // +patchStrategy=merge
//	    // +listType=map
//	    // +listMapKey=type
//	    Status []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
//
//	    // other fields
//	}
type Condition struct {
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
	// message is a human readable message indicating details about the transition.
	// This may be an empty string.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=32768
	Message string `json:"message" protobuf:"bytes,6,opt,name=message"`
	// reason contains a programmatic identifier indicating the reason for the condition's last transition.
	// Producers of specific condition types may define expected values and meanings for this field,
	// and whether the values are considered a guaranteed API.
	// The value should be a CamelCase string.
	// This field may not be empty.
	// +optional
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:Pattern=`^([A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?)?$`
	Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`
}

func (in *Condition) SetStatus(v metav1.ConditionStatus) *Condition {
	in.Status = v

	return in
}

func (in *Condition) SetMessage(v string) *Condition {
	in.Message = v

	return in
}

func (in *Condition) SetReason(reason string) *Condition {
	in.Reason = reason

	return in
}

func (in *Condition) Fail(v string) *Condition {
	in.SetStatus(metav1.ConditionFalse)
	in.SetMessage(v)

	return in
}

func NewCondition(t string, generation int64) *Condition {
	return &Condition{
		Type:               t,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

type Conditions []Condition

func (c *Conditions) Delete(p ConditionPredicate) *Conditions {
	for i, existingCondition := range *c {
		if p(existingCondition) {
			if i < len(*c)-1 {
				*c = append((*c)[:i], (*c)[i+1:]...)
			} else {
				*c = (*c)[:i]
			}
			return c
		}
	}
	return c
}

func (c *Conditions) AppendOrReplace(newCondition Condition, p ConditionPredicate) *Condition {
	c.Delete(p)
	*c = append(*c, newCondition)
	slices.SortStableFunc(*c, func(a, b Condition) int {
		switch {
		case a.Type < b.Type:
			return -1
		case a.Type > b.Type:
			return 1
		default:
			switch {
			case a.Reason < b.Reason:
				return -1
			case a.Reason > b.Reason:
				return 1
			default:
				return 0
			}
		}
	})
	return &newCondition
}

func (c *Conditions) Get(conditionType string) *Condition {
	for _, condition := range *c {
		if condition.Type == conditionType {
			return &condition
		}
	}
	return nil
}

func (c *Conditions) Check(p ConditionPredicate) bool {
	for _, condition := range *c {
		if condition.Status == metav1.ConditionTrue && p(condition) {
			return true
		}
	}
	return false
}

// +kubebuilder:object:generate=false
type ConditionPredicate func(condition Condition) bool

func AndConditions(predicates ...ConditionPredicate) ConditionPredicate {
	return func(condition Condition) bool {
		for _, predicate := range predicates {
			if !predicate(condition) {
				return false
			}
		}
		return true
	}
}

func ConditionTypeMatch(t string) ConditionPredicate {
	return func(condition Condition) bool {
		return condition.Type == t
	}
}

func ConditionReasonMatch(reason string) ConditionPredicate {
	return func(condition Condition) bool {
		return condition.Reason == reason
	}
}

func ConditionGenerationMatch(generation int64) ConditionPredicate {
	return func(condition Condition) bool {
		return condition.ObservedGeneration == generation
	}
}

type Status struct {
	//+optional
	// Ready indicates if the resource is seen as completely reconciled
	Ready bool `json:"ready"`
	//+optional
	// Info can contain any additional like reconciliation errors
	Info string `json:"info,omitempty"`
	//+optional
	Conditions Conditions `json:"conditions,omitempty"`
}

func (c *Status) SetReady(ready bool) {
	c.Ready = ready
}

func (c *Status) SetError(err string) {
	c.Info = err
}

type AuthConfig struct {
	// +optional
	ReadKeySetMaxRetries int `json:"readKeySetMaxRetries"`
	// +optional
	CheckScopes bool `json:"checkScopes"`
}

// +kubebuilder:object:generate=false
type Module interface {
	Dependent
	GetVersion() string
	IsDebug() bool
	IsDev() bool
	IsEE() bool
}

type ModuleProperties struct {
	DevProperties `json:",inline"`
	//+optional
	// Version allow to override global version defined at stack level for a specific module
	Version string `json:"version,omitempty"`
}

func (in *ModuleProperties) CompareVersion(stack *Stack, version string) int {
	actualVersion := in.Version
	if actualVersion == "" {
		actualVersion = stack.Spec.Version
	}
	if !semver.IsValid(actualVersion) {
		return 1
	}

	return semver.Compare(actualVersion, version)
}

// +kubebuilder:object:generate=false
type Dependent interface {
	Object
	GetStack() string
}

type StackDependency struct {
	// Stack indicates the stack on which the module is installed
	Stack string `json:"stack,omitempty" yaml:"-"`
}

func (d StackDependency) GetStack() string {
	return d.Stack
}

// +kubebuilder:object:generate=false
type Object interface {
	client.Object
	SetReady(bool)
	IsReady() bool
	SetError(string)
	GetConditions() *Conditions
}

// +kubebuilder:object:generate=false
type Resource interface {
	Dependent
	isResource()
}

// +k8s:openapi-gen=true
// +kubebuilder:validation:Type=string
type URI struct {
	*url.URL `json:"-"`
}

func (u URI) String() string {
	if u.URL == nil {
		return "nil"
	}
	return u.URL.String()
}

func (u URI) IsZero() bool {
	return u.URL == nil
}

func (u *URI) DeepCopyInto(v *URI) {
	cp := *u.URL
	if u.User != nil {
		cp.User = pointer.For(*u.User)
	}
	v.URL = pointer.For(cp)
}

func (u *URI) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, u.String())), nil
}

func (u *URI) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}

	v, err := url.Parse(s)
	if err != nil {
		panic(err)
	}

	*u = URI{
		URL: v,
	}
	return nil
}

func (in *URI) WithoutQuery() *URI {
	cp := *in.URL
	cp.ForceQuery = false
	cp.RawQuery = ""
	return &URI{
		URL: &cp,
	}
}

func ParseURL(v string) (*URI, error) {
	ret, err := url.Parse(v)
	if err != nil {
		return nil, err
	}
	return &URI{
		URL: ret,
	}, nil
}

func init() {
	if err := equality.Semantic.AddFunc(func(a, b *URI) bool {
		if a == nil && b != nil {
			return false
		}
		if a != nil && b == nil {
			return false
		}
		if a == nil && b == nil {
			return true
		}
		return a.String() == b.String()
	}); err != nil {
		panic(err)
	}
}

const (
	StackLabel          = "formance.com/stack"
	SkipLabel           = "formance.com/skip"
	CreatedByAgentLabel = "formance.com/created-by-agent"
)
