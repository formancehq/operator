package clients

import (
	"github.com/numary/formance-operator/apis/auth.components/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scope reconciler", func() {
	When("Creating a new client object", func() {
		var firstClient *v1beta1.Client
		BeforeEach(func() {
			firstClient = newClient()
			Expect(nsClient.Create(ctx, firstClient)).To(BeNil())
			EventuallyClientSynchronized(firstClient)
		})
		It("Should create a new client on auth server", func() {
			EventuallyApiHaveClientsLength(1)
			Expect(firstClient.Status.AuthServerID).NotTo(BeNil())
			Expect(api.clients[firstClient.Status.AuthServerID]).NotTo(BeNil())
		})
		Context("Then deleting it", func() {
			BeforeEach(func() {
				EventuallyApiHaveClientsLength(1)
				Expect(nsClient.Delete(ctx, firstClient)).To(BeNil())
			})
			It("Should be remove on auth server", func() {
				EventuallyApiHaveClientsLength(0)
			})
		})
	})
})
