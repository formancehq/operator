package streams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal"
	. "github.com/numary/formance-operator/internal/collectionutil"
	"github.com/numary/formance-operator/internal/finalizerutil"
	pkgError "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
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

func (s *StreamMutator) SetupWithBuilder(mgr ctrl.Manager, blder *ctrl.Builder) error {
	if err := mgr.
		GetFieldIndexer().
		IndexField(context.Background(), &Stream{}, ".spec.ref", func(rawObj client.Object) []string {
			return []string{rawObj.(*Stream).Spec.Reference}
		}); err != nil {
		return err
	}
	blder.Watches(
		&source.Kind{Type: &Server{}},
		handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			list := &StreamList{}
			if err := mgr.GetClient().List(context.Background(), list,
				&client.ListOptions{
					Namespace:     object.GetNamespace(),
					FieldSelector: fields.OneTermEqualSelector(".spec.ref", object.(*Server).Name),
				}); err != nil {
				mgr.GetLogger().Error(err, "Retrieving streams which reference server", "name", object.(*Server).Name)
				return nil
			}
			mgr.GetLogger().Info(fmt.Sprintf("Found %d items to reconcile", len(list.Items)))
			return Map(list.Items, func(stream Stream) reconcile.Request {
				mgr.GetLogger().Info("Trigger reconcile", "namespace",
					object.GetNamespace(), "name", object.GetName())
				return reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: stream.GetNamespace(),
						Name:      stream.GetName(),
					},
				}
			})
		}),
	)
	return nil
}

func (s *StreamMutator) Mutate(ctx context.Context, stream *Stream) (*ctrl.Result, error) {

	server := &Server{}
	if err := client.IgnoreNotFound(s.client.Get(ctx, types.NamespacedName{
		Namespace: stream.Namespace,
		Name:      stream.Spec.Reference,
	}, server)); err != nil {
		return nil, pkgError.Wrap(err, "Finding benthos server")
	}

	if server.Status.PodIP == "" {
		SetError(stream, errors.New("no ip on server"))
		return Requeue(5 * time.Second), nil
	}

	address := fmt.Sprintf("http://%s:4195", server.Status.PodIP)

	// Handle finalizer
	if isHandledByFinalizer, err := streamFinalizer.Handle(ctx, s.client, stream, func() error {
		if server.UID == "" { // Server not found, consider the stream implicitely removed
			return nil
		}
		if err := s.api.DeleteStream(ctx, address, stream.Name); err != nil && err != ErrNotFound {
			return err
		}
		return nil
	}); err != nil || isHandledByFinalizer {
		return nil, err
	}

	if server.UID == "" {
		return Requeue(), pkgError.New("Server not found")
	}

	SetProgressing(stream)

	configAsMap := make(map[string]any)
	if err := json.Unmarshal(stream.Spec.Config, &configAsMap); err != nil {
		return nil, err
	}

	benthosStream, err := s.api.GetStream(ctx, address, stream.Name)
	if err != nil && err != ErrNotFound {
		return Requeue(), err
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
	if err != nil {
		SetError(stream, err)
		if IsLintError(err) {
			return nil, nil // No requeue as it will not change anything
		}
		return Requeue(5 * time.Second), nil
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
