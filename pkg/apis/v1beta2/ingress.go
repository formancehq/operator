// +kubebuilder:object:generate=true
package v1beta2

import (
	"github.com/numary/operator/pkg/apis/v1beta1"
)

type IngressSpec struct {
	// +optional
	Annotations map[string]string `json:"annotations"`
	Path        string            `json:"path"`
	Host        string            `json:"host"`
	// +optional
	TLS *v1beta1.IngressTLS `json:"tls"`
}
