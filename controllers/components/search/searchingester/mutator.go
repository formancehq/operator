package searchingester

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/formancehq/operator/apis/components/benthos/v1beta1"
	. "github.com/formancehq/operator/apis/components/v1beta1"
	. "github.com/formancehq/operator/apis/sharedtypes"
	"github.com/formancehq/operator/internal"
	"github.com/formancehq/operator/internal/resourceutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//+kubebuilder:rbac:groups=components.formance.com,resources=searchingesters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=components.formance.com,resources=searchingesters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=components.formance.com,resources=searchingesters/finalizers,verbs=update

type Mutator struct {
	client client.Client
	scheme *runtime.Scheme
}

func (m *Mutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.Owns(&v1beta1.Stream{})
	return nil
}

func (m *Mutator) Mutate(ctx context.Context, ingester *SearchIngester) (*ctrl.Result, error) {

	SetProgressing(ingester)

	search := &Search{}
	if err := m.client.Get(ctx, types.NamespacedName{
		Namespace: ingester.Namespace,
		Name:      ingester.Spec.Reference,
	}, search); err != nil {
		return Requeue(), err
	}

	_, ret, err := resourceutil.CreateOrUpdateWithController(ctx, m.client, m.scheme, types.NamespacedName{
		Namespace: ingester.Namespace,
		Name:      ingester.Name + "-ingestion-stream",
	}, ingester, func(t *v1beta1.Stream) error {
		data, err := json.Marshal(ingester.Spec.Pipeline)
		if err != nil {
			return err
		}

		t.Spec.Config = data
		t.Spec.Reference = fmt.Sprintf("%s-search-benthos", t.Namespace)
		return nil
	})
	switch {
	case err != nil:
		SetCondition(ingester, "IngestionStreamReady", metav1.ConditionFalse, err.Error())
		return Requeue(), err
	case ret == controllerutil.OperationResultNone:
	default:
		SetCondition(ingester, "IngestionStreamReady", metav1.ConditionTrue)
	}

	SetReady(ingester)
	return nil, nil
}

var _ internal.Mutator[*SearchIngester] = &Mutator{}

func NewMutator(client client.Client, scheme *runtime.Scheme) internal.Mutator[*SearchIngester] {
	return &Mutator{
		client: client,
		scheme: scheme,
	}
}
