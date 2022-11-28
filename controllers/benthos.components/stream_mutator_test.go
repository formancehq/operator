package benthos_components

import (
	"encoding/json"

	"github.com/google/uuid"
	benthosv1beta2 "github.com/numary/operator/apis/benthos.components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	. "github.com/numary/operator/pkg/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Stream Controller", func() {
	api := newInMemoryApi()
	mutator := NewStreamMutator(GetClient(), GetScheme(), api)
	AfterEach(func() {
		api.reset()
	})

	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			var server *benthosv1beta2.Server
			BeforeEach(func() {
				server = &benthosv1beta2.Server{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
				}
				Expect(Create(server)).To(Succeed())
				server.Status.PodIP = "10.0.0.1"
				Expect(UpdateStatus(server)).To(Succeed())
			})
			AfterEach(func() {
				Expect(kClient.IgnoreNotFound(Delete(server))).To(Succeed())
				Eventually(Exists(server)).Should(BeFalse())
			})
			Context("When creating a new stream", func() {
				var stream *benthosv1beta2.Stream
				BeforeEach(func() {
					stream = &benthosv1beta2.Stream{
						ObjectMeta: metav1.ObjectMeta{
							Name: uuid.NewString(),
						},
						Spec: benthosv1beta2.StreamSpec{
							Reference: server.Name,
							Config:    json.RawMessage(`{}`),
						},
					}
					Expect(Create(stream)).To(Succeed())
					Eventually(ConditionStatus(stream, apisv1beta1.ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a stream benthos side", func() {
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
				Context("then removing the server", func() {
					BeforeEach(func() {
						Expect(Delete(server)).To(Succeed())
						Eventually(Exists(server)).Should(BeFalse())
					})
					It("Should set stream to error state", func() {
						Eventually(ConditionStatus(stream, apisv1beta1.ConditionTypeError)).Should(Equal(metav1.ConditionTrue))
					})
					Context("Then removing the stream", func() {
						BeforeEach(func() {
							Expect(Delete(stream)).To(Succeed())
						})
						It("Should be ok", func() {
							Eventually(Exists(stream)).Should(BeFalse())
						})
					})
				})
			})
		})
	})
})
