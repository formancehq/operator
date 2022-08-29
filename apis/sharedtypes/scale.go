package sharedtypes

type Scalable struct {
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
}

func (s Scalable) GetReplicas() *int32 {
	if s.Replicas != nil {
		return s.Replicas
	} else {
		replicas := int32(1)
		return &replicas
	}
}
