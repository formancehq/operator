// +kubebuilder:object:generate=true
package sharedtypes

type IngressSpec struct {
	Path string `json:"path"`
	Host string `json:"host"`
	// +optional
	Annotations map[string]string `json:"annotations"`
}
