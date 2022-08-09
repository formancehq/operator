package scopes

import (
	"github.com/google/uuid"
	"github.com/numary/auth/authclient"
	authcomponentsbeta1 "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func condition(object *authcomponentsbeta1.Scope, conditionType string) func() *authcomponentsbeta1.ConditionScope {
	return func() *authcomponentsbeta1.ConditionScope {
		err := nsClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil {
			return nil
		}
		return object.Condition(conditionType)
	}
}

func conditionStatus(object *authcomponentsbeta1.Scope, conditionType string) func() metav1.ConditionStatus {
	return func() metav1.ConditionStatus {
		c := condition(object, conditionType)()
		if c == nil {
			return metav1.ConditionUnknown
		}
		return c.Status
	}
}

var _ = Describe("Scope reconciler", func() {
	newScope := func(transient ...string) *authcomponentsbeta1.Scope {
		return authcomponentsbeta1.NewScope(uuid.NewString(), uuid.NewString(), transient...)
	}
	When("Creating a new scope object", func() {
		var firstScope *authcomponentsbeta1.Scope
		BeforeEach(func() {
			firstScope = newScope()
			Expect(nsClient.Create(ctx, firstScope)).To(BeNil())
			Eventually(conditionStatus(firstScope, authcomponentsbeta1.ConditionTypeScopesProgressing)).
				Should(Equal(metav1.ConditionFalse))
		})
		It("Should create a new scope on auth server", func() {
			Expect(api.Scopes()).To(HaveLen(1))
			Expect(firstScope.Status.AuthServerID).NotTo(BeNil())
			Expect(api.Scope(firstScope.Status.AuthServerID)).NotTo(BeNil())
			Expect(api.Scope(firstScope.Status.AuthServerID).Label).To(Equal(firstScope.Spec.Label))
		})
		Context("Then updating with a new label", func() {
			BeforeEach(func() {
				firstScope.Spec.Label = uuid.NewString()
				Expect(nsClient.Update(ctx, firstScope)).To(BeNil())
			})
			It("Should update the label on auth server", func() {
				Eventually(func() bool {
					return api.Scope(firstScope.Status.AuthServerID).Label == firstScope.Spec.Label
				}).Should(BeTrue())
			})
		})
		Context("Then deleting it", func() {
			BeforeEach(func() {
				Expect(api.Scopes()).To(HaveLen(1))
				Expect(nsClient.Delete(ctx, firstScope)).To(BeNil())
			})
			It("Should remove the scope auth server side", func() {
				Eventually(func() map[string]*authclient.Scope {
					return api.Scopes()
				}).Should(BeEmpty())
			})
		})
		Context("Then creating a new scope with the first as transient", func() {
			var secondScope *authcomponentsbeta1.Scope
			BeforeEach(func() {
				secondScope = newScope(firstScope.Name)
				Expect(nsClient.Create(ctx, secondScope)).To(BeNil())
				Eventually(conditionStatus(secondScope, authcomponentsbeta1.ConditionTypeScopesProgressing)).
					Should(Equal(metav1.ConditionFalse))
			})
			It("Should create scope with transient on auth server", func() {
				Expect(api.Scopes()).To(HaveLen(2))
				Expect(api.Scope(secondScope.Status.AuthServerID).Transient).To(Equal([]string{
					firstScope.Status.AuthServerID,
				}))
			})
			Context("Then removing transient scope", func() {
				BeforeEach(func() {
					Expect(api.Scope(secondScope.Status.AuthServerID).Transient).To(Equal([]string{
						firstScope.Status.AuthServerID,
					}))
					secondScope.Spec.Transient = make([]string, 0)
					Expect(nsClient.Update(ctx, secondScope)).To(BeNil())
				})
				It("Should remove transient scope auth server side", func() {
					Eventually(func() []string {
						return api.Scope(secondScope.Status.AuthServerID).Transient
					}).Should(BeEmpty())
				})
			})
		})
	})
})
