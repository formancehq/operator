package benthos

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	"github.com/numary/formance-operator/internal"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=benthos.components.formance.com,resources=streams/finalizers,verbs=update

type StreamMutator struct {
	client client.Client
	scheme *runtime.Scheme
}

func (s *StreamMutator) SetupWithBuilder(builder *ctrl.Builder) {}

func (s *StreamMutator) Mutate(ctx context.Context, t *Stream) (*ctrl.Result, error) {

	t.SetProgressing()

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:4195/streams/%s", t.Spec.Reference, t.Namespace, t.Name),
		bytes.NewBufferString(t.Spec.Config))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	switch rsp.StatusCode {
	case http.StatusOK, http.StatusBadRequest: // Benthos responds with 400 if stream already exists
	default:
		data, err := io.ReadAll(rsp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected status code %d: %s", rsp.StatusCode, string(data))
	}
	t.SetReady()

	return nil, nil
}

var _ internal.Mutator[StreamCondition, *Stream] = &StreamMutator{}

func NewStreamMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[StreamCondition, *Stream] {
	return &StreamMutator{
		client: client,
		scheme: scheme,
	}
}
