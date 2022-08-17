package benthos

import (
	"context"
	"fmt"
	"reflect"

	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams/finalizers,verbs=update

type StreamMutator struct {
	client client.Client
	scheme *runtime.Scheme
	api    Api
}

func (s *StreamMutator) SetupWithBuilder(builder *ctrl.Builder) {}

// TODO: Add finalizer
func (s *StreamMutator) Mutate(ctx context.Context, stream *Stream) (*ctrl.Result, error) {

	SetProgressing(stream)

	address := fmt.Sprintf("http://%s.%s.svc.cluster.local:4195", stream.Spec.Reference, stream.Namespace)

	// TODO: Should be a map on the stream object
	configAsMap := make(map[string]any)
	if err := yaml.Unmarshal([]byte(stream.Spec.Config), &configAsMap); err != nil {
		return nil, err
	}

	benthosStream, err := s.api.GetStream(ctx, address, stream.Name)
	if err != nil && err != ErrNotFound {
		return nil, err
	}
	switch {
	case benthosStream != nil && !reflect.DeepEqual(configAsMap, benthosStream.Config):
		log.FromContext(ctx).Info("Detect config stream changed, updating benthos side")
		err = s.api.UpdateStream(ctx, address, stream.Name, stream.Spec.Config)
	case benthosStream == nil:
		log.FromContext(ctx).Info("Detect stream not existing benthos side, creating it")
		err = s.api.CreateStream(ctx, address, stream.Name, stream.Spec.Config)
	default:
		log.FromContext(ctx).Info("No modification done")
		err = nil
	}
	if err != nil {
		return nil, err
	}

	SetReady(stream)

	return nil, nil
}

var _ internal.Mutator[*Stream] = &StreamMutator{}

func NewStreamMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*Stream] {
	return &StreamMutator{
		client: client,
		scheme: scheme,
	}
}
