package stack

import (
	"github.com/google/uuid"
	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/stack/v1beta1"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Stack controller", func() {
	Context("When creating stack", func() {
		var (
			stack *Stack
		)
		BeforeEach(func() {
			name := uuid.NewString()
			stack = &Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: StackSpec{
					Namespace: name,
				},
			}

			Expect(k8sClient.Create(ctx, stack)).To(Succeed())
			Eventually(ConditionStatus[StackCondition](k8sClient, stack, ConditionTypeStackReady)).
				Should(Equal(metav1.ConditionTrue))
		})
		It("Should create a new namespace", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: stack.Spec.Namespace,
			}, &v1.Namespace{})).To(BeNil())
		})
		Context("With auth configuration", func() {
			BeforeEach(func() {
				stack.Spec.Auth = &AuthSpec{
					PostgresConfig: componentsv1beta1.PostgresConfig{
						Database: "test",
						Port:     5432,
						Host:     "postgres",
						Username: "admin",
						Password: "admin",
					},
					SigningKey: "XXX",
					DelegatedOIDCServer: componentsv1beta1.DelegatedOIDCServerConfiguration{
						Issuer:       "http://example.net",
						ClientID:     "clientId",
						ClientSecret: "clientSecret",
					},
				}
				Expect(k8sClient.Update(ctx, stack)).To(BeNil())
				Eventually(ConditionStatus[StackCondition](k8sClient, stack, ConditionTypeStackAuthCreated)).
					Should(Equal(metav1.ConditionTrue))
			})
			It("Should create a auth server on a new namespace", func() {
				Expect(Exists(k8sClient, &componentsv1beta1.Auth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      stack.Spec.Auth.Name(stack),
						Namespace: stack.Spec.Namespace,
					},
				})()).To(BeTrue())
			})
			Context("Then removing auth", func() {
				BeforeEach(func() {
					stack.Spec.Auth = nil
					Expect(k8sClient.Update(ctx, stack)).To(BeNil())
					Eventually(ConditionStatus[StackCondition](k8sClient, stack, ConditionTypeStackAuthCreated)).Should(Equal(metav1.ConditionUnknown))
				})
				It("Should remove Auth deployment", func() {
					Expect(Exists(k8sClient, &componentsv1beta1.Auth{
						ObjectMeta: metav1.ObjectMeta{
							Name:      stack.Name,
							Namespace: stack.Spec.Namespace,
						},
					})()).To(BeFalse())
				})
			})
		})
	})
})
