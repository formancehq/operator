package streams

import (
	"encoding/json"

	"github.com/google/uuid"
	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Stream Controller", func() {
	Context("When creating a new stream", func() {
		var stream *Stream
		BeforeEach(func() {
			stream = &Stream{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
				Spec: StreamSpec{
					Reference: "foo",
					Config:    json.RawMessage(`{}`),
				},
			}
			Expect(nsClient.Create(ctx, stream)).To(Succeed())
			Eventually(ConditionStatus(nsClient, stream, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
		})
		It("Should create a stream benthos side", func() {
			Eventually(func() int {
				return len(api.configs)
			}).Should(Equal(1))
		})
		Context("then removing it when ready", func() {
			BeforeEach(func() {
				Expect(nsClient.Delete(ctx, stream)).To(Succeed())
				Eventually(Exists(nsClient, stream)).Should(BeFalse())
			})
			It("Should remove benthos side", func() {
				Eventually(api.configs).Should(BeEmpty())
			})
		})
	})
})
