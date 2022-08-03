package scopes

import (
	"github.com/google/uuid"
	"github.com/numary/formance-operator/apis/auth.components/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scope reconciler", func() {
	When("Creating a new scope object", func() {
		var scope *v1beta1.Scope
		BeforeEach(func() {
			scope = newScope()
			Expect(nsClient.Create(ctx, scope)).To(BeNil())
			EventuallyScopeSynchronized(scope)
		})
		It("Should create a new scope on auth server", func() {
			EventuallyApiHaveScopeLength(1)
			Expect(scope.Status.AuthServerID).NotTo(BeNil())
			Expect(api.scopes[scope.Status.AuthServerID]).NotTo(BeNil())
			Expect(api.scopes[scope.Status.AuthServerID].Label).To(Equal(scope.Spec.Label))
		})
		Context("Then updating with a new label", func() {
			var updatedLabel string
			BeforeEach(func() {
				updatedLabel = uuid.NewString()
				scope.Spec.Label = updatedLabel
				Expect(nsClient.Update(ctx, scope)).To(BeNil())
			})
			It("Should update the label on auth server", func() {
				Eventually(func() bool {
					return api.scopes[scope.Status.AuthServerID].Label == updatedLabel
				}).Should(BeTrue())
			})
		})
		Context("Then deleting it", func() {
			BeforeEach(func() {
				EventuallyApiHaveScopeLength(1)
				Expect(nsClient.Delete(ctx, scope)).To(BeNil())
			})
			It("Should remove the scope auth server side", func() {
				EventuallyApiHaveScopeLength(0)
			})
		})
	})
})
