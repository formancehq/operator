package searchingester

import (
	"encoding/json"
	"fmt"

	"github.com/formancehq/operator/apis/components/benthos/v1beta1"
	. "github.com/formancehq/operator/apis/components/v1beta1"
	. "github.com/formancehq/operator/apis/sharedtypes"
	. "github.com/formancehq/operator/internal/testing"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Test Search Ingester", func() {
	mutator := NewMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			var search *Search
			BeforeEach(func() {
				search = &Search{
					ObjectMeta: metav1.ObjectMeta{
						Name: uuid.NewString(),
					},
					Spec: SearchSpec{
						ElasticSearch: ElasticSearchConfig{
							Host:   "elastic",
							Scheme: "http",
							Port:   9200,
						},
						KafkaConfig: KafkaConfig{
							Brokers: []string{"kafka"},
							TLS:     false,
						},
						Index: "documents",
					},
				}
				Expect(Create(search)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(search)).Should(Succeed())
			})
			Context("With a search deployed, when creating a new SearchIngester object", func() {
				var searchIngester *SearchIngester
				BeforeEach(func() {
					searchIngester = &SearchIngester{
						ObjectMeta: metav1.ObjectMeta{
							Name: uuid.NewString(),
						},
						Spec: SearchIngesterSpec{
							Reference: search.Name,
							Pipeline:  json.RawMessage(`{"foo": "bar"}`),
						},
					}
					Expect(Create(searchIngester)).To(Succeed())
					Eventually(ConditionStatus(searchIngester, ConditionTypeReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a BenthosStream object", func() {
					stream := &v1beta1.Stream{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("%s-ingestion-stream", searchIngester.Name),
						},
					}
					Eventually(Exists(stream)).Should(BeTrue())
				})
			})
		})
	})
})
