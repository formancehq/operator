package clients

import (
	"time"

	"github.com/google/uuid"
	"github.com/numary/formance-operator/apis/auth.components/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func EventuallyGeneration(object client.Object) AsyncAssertion {
	return Eventually(func() int64 {
		_ = nsClient.Get(ctx, types.NamespacedName{
			Name: object.GetName(),
		}, object)
		return object.GetGeneration()
	})
}

var _ = Describe("Scope reconciler", func() {
	When("Creating a new client object", func() {
		var client *v1beta1.Client
		BeforeEach(func() {
			client = newClient()
			Expect(nsClient.Create(ctx, client)).To(BeNil())
			EventuallyGeneration(client).Should(Equal(client.Generation + 1))
		})
		FIt("Should create a new client on auth server", func() {
			EventuallyApiHaveClientsLength(1)
			Expect(client.Status.AuthServerID).NotTo(BeNil())
			Expect(api.clients[client.Status.AuthServerID]).NotTo(BeNil())
			Expect(api.clients[client.Status.AuthServerID].Name).To(Equal(client.Name))
		})
		Context("Then deleting it", func() {
			BeforeEach(func() {
				EventuallyApiHaveClientsLength(1)
				Expect(nsClient.Delete(ctx, client)).To(BeNil())
			})
			It("Should be remove on auth server", func() {
				EventuallyApiHaveClientsLength(0)
			})
		})
		Context("Then adding a scope", func() {
			var scope *v1beta1.Scope
			BeforeEach(func() {
				scope = v1beta1.NewScope(uuid.NewString(), uuid.NewString())
				Expect(nsClient.Create(ctx, scope)).To(BeNil())
				scope.Status.AuthServerID = "XXX"
				Expect(nsClient.Status().Update(ctx, scope)).To(BeNil())

				client.AddScopeSpec(scope)
				Expect(nsClient.Update(ctx, client)).To(BeNil())

				EventuallyGeneration(client).Should(Equal(client.Generation + 1))
			})
			It("Should add the scope auth server side", func() {
				EventuallyApiHaveClientsLength(1)
				Eventually(func() bool {
					client := api.clients[client.Status.AuthServerID]
					return len(client.Scopes) == 1 && client.Scopes[0] == "XXX"
				}).Should(BeTrue())
			})
			Context("Then deleting the scope", func() {
				BeforeEach(func() {
					EventuallyApiHaveClientsLength(1)
					Eventually(func() bool {
						client := api.clients[client.Status.AuthServerID]
						return len(client.Scopes) == 1 && client.Scopes[0] == "XXX"
					}).Should(BeTrue())
					client.Spec.Scopes = []string{}
					Expect(nsClient.Update(ctx, client)).To(BeNil())
				})
				It("Should remove the scope on auth server", func() {
					EventuallyApiHaveClientsLength(1)
					Eventually(func() bool {
						return len(api.clients[client.Status.AuthServerID].Scopes) == 0
					}).WithTimeout(2 * time.Second).Should(BeTrue())
				})
			})
		})
	})
})
