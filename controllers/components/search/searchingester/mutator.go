package searchingester

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/internal"
	"github.com/numary/formance-operator/internal/resourceutil"
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

func (m *Mutator) SetupWithBuilder(builder *ctrl.Builder) {}

func (m *Mutator) Mutate(ctx context.Context, ingester *SearchIngester) (*ctrl.Result, error) {

	SetProgressing(ingester)

	search := &Search{}
	if err := m.client.Get(ctx, types.NamespacedName{
		Namespace: ingester.Namespace,
		Name:      ingester.Spec.Reference,
	}, search); err != nil {
		return nil, err
	}

	_, ret, err := resourceutil.CreateOrUpdateWithController(ctx, m.client, m.scheme, types.NamespacedName{
		Namespace: ingester.Namespace,
		Name:      ingester.Name + "-ingestion-stream",
	}, ingester, func(t *v1beta1.Stream) error {
		config := map[string]interface{}{
			"input": map[string]any{
				"kafka": map[string]any{
					"addresses": search.Spec.KafkaConfig.Brokers,
					"topics":    []string{ingester.Spec.Topic},
					//"target_version":   "",       //TODO
					"consumer_group":   "search", // TODO
					"checkpoint_limit": 1024,     // TODO ?
				},
			},
			"pipeline": ingester.Spec.Pipeline,
			"output": map[string]any{
				"processors": []map[string]any{
					{
						"bloblang": `root = "%s\n".format(this.map_each(v -> v.string()).join("\n"))`,
					},
				},
				"broker": map[string]any{
					"outputs": []map[string]any{
						{"stdout": map[string]any{}}, //TODO: Only in dev mode
						{
							"http_client": map[string]any{
								"url": fmt.Sprintf("%s://%s:%d/_bulk",
									search.Spec.ElasticSearch.Scheme,
									search.Spec.ElasticSearch.Host,
									search.Spec.ElasticSearch.Port),
								"verb": "POST",
								"headers": map[string]any{
									"Content-Type": "application/x-ndjson",
								},
								"tls": map[string]any{ // TODO: Make configurable
									"enabled":          false,
									"skip_cert_verify": false,
								},
								"basic_auth": map[string]any{ //TODO: Make configurable
									"enabled":  true,
									"username": "admin",
									"password": "admin",
								},
							},
						},
					},
				},
			},
		}

		data, err := json.Marshal(config)
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
		return nil, err
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
