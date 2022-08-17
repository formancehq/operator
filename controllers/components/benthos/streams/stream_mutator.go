package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal"
	"github.com/numary/formance-operator/pkg/finalizerutil"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var streamFinalizer = finalizerutil.New("streams.benthos.components.formance.com/finalizer")

//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams/finalizers,verbs=update

type StreamMutator struct {
	client client.Client
	scheme *runtime.Scheme
	api    Api
}

func (s *StreamMutator) SetupWithBuilder(builder *ctrl.Builder) {}

func (s *StreamMutator) Mutate(ctx context.Context, stream *Stream) (*ctrl.Result, error) {

	address := fmt.Sprintf("http://%s.%s.svc.cluster.local:4195", stream.Spec.Reference, stream.Namespace)

	// Handle finalizer
	if isHandledByFinalizer, err := streamFinalizer.Handle(ctx, s.client, stream, func() error {
		if err := s.api.DeleteStream(ctx, address, stream.Name); err != nil && err != ErrNotFound {
			return err
		}
		return nil
	}); err != nil || isHandledByFinalizer {
		return nil, err
	}

	SetProgressing(stream)

	configAsMap := make(map[string]any)
	if err := json.Unmarshal(stream.Spec.Config, &configAsMap); err != nil {
		return nil, err
	}

	benthosStream, err := s.api.GetStream(ctx, address, stream.Name)
	if err != nil && err != ErrNotFound {
		return nil, err
	}
	switch {
	case benthosStream != nil && !reflect.DeepEqual(configAsMap, benthosStream.Config):
		log.FromContext(ctx).Info("Detect config stream changed, updating benthos side")
		err = s.api.UpdateStream(ctx, address, stream.Name, string(stream.Spec.Config))
	case benthosStream == nil:
		log.FromContext(ctx).Info("Detect stream not existing benthos side, creating it")
		err = s.api.CreateStream(ctx, address, stream.Name, string(stream.Spec.Config))
	default:
		log.FromContext(ctx).Info("No modification done")
		err = nil
	}
	// TODO: Handle Lint error, retrying will not work anyway
	// TODO: More generally we have to split errors between errors which can be recovered (trigger a new reconciliation)
	// and errors which will not change even after a new reconciliation (lint errors are)
	if err != nil {
		return nil, err
	}

	SetReady(stream)

	return nil, nil
}

var _ internal.Mutator[*Stream] = &StreamMutator{}

func NewStreamMutator(client client.Client, scheme *runtime.Scheme, api Api) internal.Mutator[*Stream] {
	return &StreamMutator{
		client: client,
		scheme: scheme,
		api:    api,
	}
}
