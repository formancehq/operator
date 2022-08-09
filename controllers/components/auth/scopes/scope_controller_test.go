package scopes

import (
	"github.com/google/uuid"
	"github.com/numary/formance-operator/apis/components/auth/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scope reconciler", func() {
	When("Creating a new scope object", func() {
		var firstScope *v1beta1.Scope
		BeforeEach(func() {
			firstScope = newScope()
			Expect(nsClient.Create(ctx, firstScope)).To(BeNil())
			EventuallyScopeSynchronized(firstScope)
		})
		It("Should create a new scope on auth server", func() {
			EventuallyApiHaveScopeLength(1)
			Expect(firstScope.Status.AuthServerID).NotTo(BeNil())
			Expect(api.scopes[firstScope.Status.AuthServerID]).NotTo(BeNil())
			Expect(api.scopes[firstScope.Status.AuthServerID].Label).To(Equal(firstScope.Spec.Label))
		})
		Context("Then updating with a new label", func() {
			BeforeEach(func() {
				firstScope.Spec.Label = uuid.NewString()
				Expect(nsClient.Update(ctx, firstScope)).To(BeNil())
			})
			It("Should update the label on auth server", func() {
				Eventually(func() bool {
					return api.scopes[firstScope.Status.AuthServerID].Label == firstScope.Spec.Label
				}).Should(BeTrue())
			})
		})
		Context("Then deleting it", func() {
			BeforeEach(func() {
				EventuallyApiHaveScopeLength(1)
				Expect(nsClient.Delete(ctx, firstScope)).To(BeNil())
			})
			It("Should remove the scope auth server side", func() {
				EventuallyApiHaveScopeLength(0)
			})
		})
		Context("Then creating a new scope with the first as transient", func() {
			var secondScope *v1beta1.Scope
			BeforeEach(func() {
				secondScope = newScope(firstScope.Name)
				Expect(nsClient.Create(ctx, secondScope)).To(BeNil())
				EventuallyScopeSynchronized(secondScope)
			})
			It("Should create scope with transient on auth server", func() {
				EventuallyApiHaveScopeLength(2)
				Expect(api.scopes[secondScope.Status.AuthServerID].Transient).To(Equal([]string{
					firstScope.Status.AuthServerID,
				}))
			})
			Context("Then removing transient scope", func() {
				BeforeEach(func() {
					Expect(api.scopes[secondScope.Status.AuthServerID].Transient).To(Equal([]string{
						firstScope.Status.AuthServerID,
					}))
					secondScope.Spec.Transient = make([]string, 0)
					Expect(nsClient.Update(ctx, secondScope)).To(BeNil())
				})
				It("Should remove transient scope auth server side", func() {
					Eventually(func() []string {
						return api.scopes[secondScope.Status.AuthServerID].Transient
					}).Should(BeEmpty())
				})
			})
		})
	})
})
