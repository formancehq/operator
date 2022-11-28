package components

import (
	"context"
	"encoding/json"
	"fmt"

	benthosv1beta2 "github.com/numary/operator/apis/benthos.components/v1beta2"
	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/controllerutils"
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

type SearchIngesterMutator struct {
	client client.Client
	scheme *runtime.Scheme
}

func (m *SearchIngesterMutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.Owns(&benthosv1beta2.Stream{})
	return nil
}

func (m *SearchIngesterMutator) Mutate(ctx context.Context, ingester *componentsv1beta2.SearchIngester) (*ctrl.Result, error) {

	apisv1beta1.SetProgressing(ingester)

	search := &componentsv1beta2.Search{}
	if err := m.client.Get(ctx, types.NamespacedName{
		Namespace: ingester.Namespace,
		Name:      ingester.Spec.Reference,
	}, search); err != nil {
		return controllerutils.Requeue(), err
	}

	_, ret, err := controllerutils.CreateOrUpdateWithController(ctx, m.client, m.scheme, types.NamespacedName{
		Namespace: ingester.Namespace,
		Name:      ingester.Name + "-ingestion-stream",
	}, ingester, func(t *benthosv1beta2.Stream) error {
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
		apisv1beta1.SetCondition(ingester, "IngestionStreamReady", metav1.ConditionFalse, err.Error())
		return controllerutils.Requeue(), err
	case ret == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetCondition(ingester, "IngestionStreamReady", metav1.ConditionTrue)
	}

	apisv1beta1.SetReady(ingester)
	return nil, nil
}

var _ controllerutils.Mutator[*componentsv1beta2.SearchIngester] = &SearchIngesterMutator{}

func NewSearchIngesterMutator(client client.Client, scheme *runtime.Scheme) controllerutils.Mutator[*componentsv1beta2.SearchIngester] {
	return &SearchIngesterMutator{
		client: client,
		scheme: scheme,
	}
}
