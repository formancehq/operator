package streams

import (
	"encoding/json"

	"github.com/google/uuid"
	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Stream Controller", func() {
	api := newInMemoryApi()
	mutator := NewStreamMutator(GetClient(), GetScheme(), api)
	AfterEach(func() {
		api.reset()
	})

	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			var server *Server
			BeforeEach(func() {
				server = &Server{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
					Status: ServerStatus{
						Pod:   "xxx",
						PodIP: "10.0.0.1",
					},
				}
				Expect(Create(server)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(server)).To(Succeed())
				Eventually(Exists(server)).Should(BeFalse())
			})
			Context("When creating a new stream", func() {
				var stream *Stream
				BeforeEach(func() {
					stream = &Stream{
						ObjectMeta: metav1.ObjectMeta{
							Name: uuid.NewString(),
						},
						Spec: StreamSpec{
							Reference: server.Name,
							Config:    json.RawMessage(`{}`),
						},
					}
					Expect(Create(stream)).To(Succeed())
					Eventually(ConditionStatus(stream, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				FIt("Should create a stream benthos side", func() {
					Eventually(func() int {
						return len(api.configs)
					}).Should(Equal(1))
				})
				Context("then removing it when ready", func() {
					BeforeEach(func() {
						Expect(Delete(stream)).To(Succeed())
						Eventually(Exists(stream)).Should(BeFalse())
					})
					It("Should remove benthos side", func() {
						Eventually(api.configs).Should(BeEmpty())
					})
				})
			})
		})
	})
})
