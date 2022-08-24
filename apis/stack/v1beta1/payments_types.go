package v1beta1

type MongoDBConfig struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// +kubebuilder:object:generate=true
type PaymentsSpec struct {
	// +optional
	Debug bool `json:"debug,omitempty"`
	// +optional
	Scaling ScalingSpec `json:"scaling,omitempty"`
	// +optional
	Image string `json:"image"`
	// +optional
	Ingress *IngressConfig `json:"ingress"`
	MongoDB MongoDBConfig  `json:"mongoDB"`
}
