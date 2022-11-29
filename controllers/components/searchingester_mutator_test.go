package components

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	benthoscomponentsv1beta2 "github.com/numary/operator/apis/benthos.components/v1beta2"
	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Test Search Ingester", func() {
	mutator := NewSearchIngesterMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			var search *componentsv1beta2.Search
			BeforeEach(func() {
				search = &componentsv1beta2.Search{
					ObjectMeta: metav1.ObjectMeta{
						Name: uuid.NewString(),
					},
					Spec: componentsv1beta2.SearchSpec{
						ElasticSearch: NewDumpElasticSearchConfig(),
						KafkaConfig:   NewDumpKafkaConfig(),
						Index:         "documents",
					},
				}
				Expect(Create(search)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(search)).Should(Succeed())
			})
			Context("With a search deployed, when creating a new SearchIngester object", func() {
				var searchIngester *componentsv1beta2.SearchIngester
				BeforeEach(func() {
					searchIngester = &componentsv1beta2.SearchIngester{
						ObjectMeta: metav1.ObjectMeta{
							Name: uuid.NewString(),
						},
						Spec: componentsv1beta2.SearchIngesterSpec{
							Reference: search.Name,
							Stream:    json.RawMessage(`{"foo": "bar"}`),
						},
					}
					Expect(Create(searchIngester)).To(Succeed())
					Eventually(ConditionStatus(searchIngester, apisv1beta1.ConditionTypeReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a BenthosStream object", func() {
					stream := &benthoscomponentsv1beta2.Stream{
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
