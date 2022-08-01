package reconcile

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Reconcile struct {
	client.Client
	Scheme *runtime.Scheme
}

type ScalingSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	MinReplica int `json:"minReplica,omitempty"`
	// +optional
	MaxReplica int `json:"maxReplica,omitempty"`
	// +optional
	CpuLimit int `json:"cpuLimit,omitempty"`
}

type DatabaseSpec struct {
	// +optional
	Url string `json:"url,omitempty"`
	// +optional
	Type string `json:"type,omitempty"`
}
