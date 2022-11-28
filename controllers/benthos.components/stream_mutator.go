package benthos_components

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	benthosv1beta2 "github.com/numary/operator/apis/benthos.components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/typeutils"
	pkgError "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/errors"
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

var streamFinalizer = controllerutils.New("streams.benthos.components.formance.com/finalizer")

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
		IndexField(context.Background(), &benthosv1beta2.Stream{}, ".spec.ref", func(rawObj client.Object) []string {
			return []string{rawObj.(*benthosv1beta2.Stream).Spec.Reference}
		}); err != nil {
		return err
	}
	blder.Watches(
		&source.Kind{Type: &benthosv1beta2.Server{}},
		handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			list := &benthosv1beta2.StreamList{}
			if err := mgr.GetClient().List(context.Background(), list,
				&client.ListOptions{
					Namespace:     object.GetNamespace(),
					FieldSelector: fields.OneTermEqualSelector(".spec.ref", object.(*benthosv1beta2.Server).Name),
				}); err != nil {
				mgr.GetLogger().Error(err, "Retrieving streams which reference server", "name", object.(*benthosv1beta2.Server).Name)
				return nil
			}
			mgr.GetLogger().Info(fmt.Sprintf("Found %d items to reconcile", len(list.Items)))
			return Map(list.Items, func(stream benthosv1beta2.Stream) reconcile.Request {
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

func (s *StreamMutator) Mutate(ctx context.Context, stream *benthosv1beta2.Stream) (*ctrl.Result, error) {

	apisv1beta1.RemoveReadyCondition(stream)

	server := &benthosv1beta2.Server{}
	err := s.client.Get(ctx, types.NamespacedName{
		Namespace: stream.Namespace,
		Name:      stream.Spec.Reference,
	}, server)
	if err != nil {
		switch {
		case errors.IsNotFound(err):
		default:
			return nil, pkgError.Wrap(err, "Finding benthos server")
		}
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

	if server.UID == "" { // Not found
		return controllerutils.Requeue(5 * time.Second), pkgError.New("server not found")
	}
	if server.Status.PodIP == "" {
		return controllerutils.Requeue(5 * time.Second), pkgError.New("no ip on server")
	}

	apisv1beta1.SetProgressing(stream)

	configAsMap := make(map[string]any)
	if err := json.Unmarshal(stream.Spec.Config, &configAsMap); err != nil {
		return nil, err
	}

	benthosStream, err := s.api.GetStream(ctx, address, stream.Name)
	if err != nil && err != ErrNotFound {
		return controllerutils.Requeue(), err
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
		if IsLintError(err) {
			return nil, err // No requeue as it will not change anything
		}
		return controllerutils.Requeue(5 * time.Second), err
	}

	apisv1beta1.SetReady(stream)

	return nil, nil
}

var _ controllerutils.Mutator[*benthosv1beta2.Stream] = &StreamMutator{}

func NewStreamMutator(client client.Client, scheme *runtime.Scheme, api Api) controllerutils.Mutator[*benthosv1beta2.Stream] {
	return &StreamMutator{
		client: client,
		scheme: scheme,
		api:    api,
	}
}
