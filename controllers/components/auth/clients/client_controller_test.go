package clients

import (
	"github.com/google/uuid"
	"github.com/numary/formance-operator/apis/auth.components/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Client reconciler", func() {
	When("Creating a new client object", func() {
		var actualClient *v1beta1.Client
		BeforeEach(func() {
			actualClient = newClient()
			Expect(nsClient.Create(ctx, actualClient)).To(BeNil())
			Eventually(clientReady(actualClient)).Should(BeTrue())
		})
		AfterEach(func() {
			Expect(client.IgnoreNotFound(nsClient.Delete(ctx, actualClient))).To(BeNil())
		})
		It("Should create a new client on auth server", func() {
			Expect(api.clients).To(HaveLen(1))
			Expect(actualClient.Status.AuthServerID).NotTo(BeNil())
			Expect(api.clients[actualClient.Status.AuthServerID]).NotTo(BeNil())
			Expect(api.clients[actualClient.Status.AuthServerID].Name).To(Equal(actualClient.Name))
		})
		Context("Then deleting it", func() {
			BeforeEach(func() {
				Expect(nsClient.Delete(ctx, actualClient)).To(BeNil())
				Eventually(clientNotFound(actualClient)).Should(BeTrue())
			})
			It("Should be remove on auth server", func() {
				Expect(api.clients).To(HaveLen(0))
			})
		})
		Context("Then adding an unknown scope without creating it", func() {
			var scope *v1beta1.Scope
			BeforeEach(func() {
				scope = v1beta1.NewScope(uuid.NewString(), uuid.NewString())

				actualClient.AddScopeSpec(scope)
				Expect(nsClient.Update(ctx, actualClient)).To(BeNil())
			})
			It("Should set the client to not ready state", func() {
				Eventually(clientReady(actualClient)).Should(BeFalse())
			})
			Context("Then creating the scope", func() {
				BeforeEach(func() {
					Eventually(clientReady(actualClient)).Should(BeFalse())
					Expect(nsClient.Create(ctx, scope)).To(BeNil())
					scope.Status.AuthServerID = "XXX"
					Expect(nsClient.Status().Update(ctx, scope)).To(BeNil())

					Eventually(clientReady(actualClient)).Should(BeTrue())
					Eventually(clientSynchronizedScopes(actualClient)).Should(Equal(map[string]string{
						scope.Name: scope.Status.AuthServerID,
					}))
				})
				AfterEach(func() {
					Expect(nsClient.Delete(ctx, scope)).To(BeNil())
				})
				It("Should add scopes to the auth server client", func() {
					Expect(api.clients).To(HaveLen(1))
					client := api.clients[actualClient.Status.AuthServerID]
					Expect(client.Scopes).To(HaveLen(1))
					Expect(client.Scopes[0]).To(Equal("XXX"))
				})
				Context("Then deleting the scope", func() {
					BeforeEach(func() {
						actualClient.Spec.Scopes = []string{}
						Expect(nsClient.Update(ctx, actualClient)).To(BeNil())
						Eventually(clientSynchronizedScopes(actualClient)).Should(Equal(map[string]string{}))
					})
					It("Should delete the scope auth server side", func() {
						Expect(api.clients).To(HaveLen(1))
						client := api.clients[actualClient.Status.AuthServerID]
						Expect(client.Scopes).To(HaveLen(0))
					})
				})
			})
		})
	})
})
