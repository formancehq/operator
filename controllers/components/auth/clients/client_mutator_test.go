package clients

import (
	"github.com/google/uuid"
	. "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Client reconciler", func() {

	newClient := func() *Client {
		return NewClient(uuid.NewString())
	}

	When("Creating a new client object", func() {
		var actualClient *Client
		BeforeEach(func() {
			actualClient = newClient()
			Expect(nsClient.Create(ctx, actualClient)).To(BeNil())
			Eventually(ConditionStatus(nsClient, actualClient, ConditionTypeClientCreated)).
				Should(Equal(metav1.ConditionTrue))
		})
		AfterEach(func() {
			Expect(client.IgnoreNotFound(nsClient.Delete(ctx, actualClient))).To(BeNil())
		})
		It("Should create a new client on auth server", func() {
			Expect(api.Clients()).To(HaveLen(1))
			Expect(actualClient.Status.AuthServerID).NotTo(BeNil())
			Expect(api.Client(actualClient.Status.AuthServerID)).NotTo(BeNil())
			Expect(api.Client(actualClient.Status.AuthServerID).Name).To(Equal(actualClient.Name))
		})
		Context("Then deleting it", func() {
			BeforeEach(func() {
				Expect(nsClient.Delete(ctx, actualClient)).To(BeNil())
				Eventually(NotFound(nsClient, actualClient)).Should(BeTrue())
			})
			It("Should be remove on auth server", func() {
				Expect(api.Clients()).To(HaveLen(0))
			})
		})
		Context("Then adding an unknown scope without creating it", func() {
			var scope *Scope
			BeforeEach(func() {
				scope = NewScope(uuid.NewString(), uuid.NewString())

				actualClient.AddScopeSpec(scope)
				Expect(nsClient.Update(ctx, actualClient)).To(BeNil())
			})
			It("Should set the client to not ready state", func() {
				Eventually(ConditionStatus(nsClient, actualClient, ConditionTypeClientProgressing)).
					Should(Equal(metav1.ConditionTrue))
			})
			Context("Then creating the scope", func() {
				BeforeEach(func() {
					Eventually(ConditionStatus(nsClient, actualClient, ConditionTypeClientProgressing)).
						Should(Equal(metav1.ConditionTrue))
					Expect(nsClient.Create(ctx, scope)).To(BeNil())
					scope.Status.AuthServerID = "XXX"
					Expect(nsClient.Status().Update(ctx, scope)).To(BeNil())

					Eventually(ConditionStatus(nsClient, actualClient, ConditionTypeClientProgressing)).
						Should(Equal(metav1.ConditionFalse))
					Expect(actualClient.Status.Scopes).To(Equal(map[string]string{
						scope.Name: scope.Status.AuthServerID,
					}))
				})
				AfterEach(func() {
					Expect(nsClient.Delete(ctx, scope)).To(BeNil())
				})
				It("Should add scopes to the auth server client", func() {
					Expect(api.Clients()).To(HaveLen(1))
					client := api.Client(actualClient.Status.AuthServerID)
					Expect(client.Scopes).To(HaveLen(1))
					Expect(client.Scopes[0]).To(Equal("XXX"))
				})
				Context("Then remove the scope from the client", func() {
					BeforeEach(func() {
						actualClient.Spec.Scopes = []string{}
						Expect(nsClient.Update(ctx, actualClient)).To(BeNil())
						Eventually(func() map[string]string {
							_ = nsClient.Get(ctx, client.ObjectKeyFromObject(actualClient), actualClient)
							return actualClient.Status.Scopes
						}).Should(Equal(map[string]string{}))
					})
					It("Should delete the scope auth server side", func() {
						Expect(api.Clients()).To(HaveLen(1))
						client := api.Client(actualClient.Status.AuthServerID)
						Expect(client.Scopes).To(HaveLen(0))
					})
				})
			})
		})
	})
})
