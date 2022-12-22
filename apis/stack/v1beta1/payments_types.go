package v1beta1

type MongoDBConfig struct{}

type ImageHolder struct{}

// +kubebuilder:object:generate=true
type PaymentsSpec struct {
	ImageHolder `json:",inline"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	// +optional
	MongoDB MongoDBConfig `json:"mongoDB"`
}
