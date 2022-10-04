package searchingester

import (
	"context"
	"encoding/json"
	"fmt"
	
	"github.com/numary/operator/apis/components/benthos/v1beta1"
	. "github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	"github.com/numary/operator/internal"
	"github.com/numary/operator/internal/resourceutil"
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
		httpClientOutput := map[string]any{
			"url": fmt.Sprintf("%s://%s:%d/_bulk",
				search.Spec.ElasticSearch.Scheme,
				search.Spec.ElasticSearch.Host,
				search.Spec.ElasticSearch.Port),
			"verb": "POST",
			"headers": map[string]any{
				"Content-Type": "application/x-ndjson",
			},
			"tls": map[string]any{
				"enabled":          search.Spec.ElasticSearch.TLS.Enabled,
				"skip_cert_verify": search.Spec.ElasticSearch.TLS.SkipCertVerify,
			},
		}
		if search.Spec.ElasticSearch.BasicAuth != nil {
			httpClientOutput["basic_auth"] = map[string]any{
				"enabled":  true,
				"username": search.Spec.ElasticSearch.BasicAuth.Username,
				"password": search.Spec.ElasticSearch.BasicAuth.Password,
			}
		}
		outputs := []map[string]any{{
			"http_client": httpClientOutput,
		}}
		if ingester.Spec.Debug {
			outputs = append(outputs, map[string]any{
				"stdout": map[string]any{},
			})
		}
		kafkaInput := map[string]any{
			"addresses":        search.Spec.KafkaConfig.Brokers,
			"topics":           []string{ingester.Spec.Topic},
			"consumer_group":   ingester.Name,
			"checkpoint_limit": 1024,
		}
		if search.Spec.KafkaConfig.TLS {
			kafkaInput["tls"] = map[string]any{
				"enabled": true,
			}
		}
		if search.Spec.KafkaConfig.SASL != nil {
			kafkaInput["sasl"] = map[string]any{
				"mechanism": search.Spec.KafkaConfig.SASL.Mechanism,
				"user":      search.Spec.KafkaConfig.SASL.Username,
				"password":  search.Spec.KafkaConfig.SASL.Password,
			}
		}
		config := map[string]interface{}{
			"input": map[string]any{
				"kafka": kafkaInput,
			},
			"pipeline": ingester.Spec.Pipeline,
			"output": map[string]any{
				"processors": []map[string]any{
					{
						"bloblang": `root = "%s\n".format(this.map_each(v -> v.string()).join("\n"))`,
					},
				},
				"broker": map[string]any{
					"outputs": outputs,
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
