package stack

import (
	"github.com/google/uuid"
	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	"github.com/numary/formance-operator/apis/stack/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func condition(object *v1beta1.Stack, conditionType string) func() *v1beta1.StackCondition {
	return func() *v1beta1.StackCondition {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil {
			return nil
		}
		return object.Condition(conditionType)
	}
}

func conditionStatus(object *v1beta1.Stack, conditionType string) func() metav1.ConditionStatus {
	return func() metav1.ConditionStatus {
		c := condition(object, conditionType)()
		if c == nil {
			return metav1.ConditionUnknown
		}
		return c.Status
	}
}

func exists(key client.ObjectKey, object client.Object) func() bool {
	return func() bool {
		return k8sClient.Get(ctx, key, object) == nil
	}
}

var _ = Describe("Stack controller", func() {
	Context("When creating stack", func() {
		var (
			stack *v1beta1.Stack
		)
		BeforeEach(func() {
			name := uuid.NewString()
			stack = &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: v1beta1.StackSpec{
					Namespace: name,
				},
			}

			Expect(k8sClient.Create(ctx, stack)).To(Succeed())
			Eventually(conditionStatus(stack, v1beta1.ConditionTypeStackReady)).Should(Equal(metav1.ConditionTrue))
		})
		It("Should create a new namespace", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: stack.Spec.Namespace,
			}, &v1.Namespace{})).To(BeNil())
		})
		Context("With auth configuration", func() {
			BeforeEach(func() {
				stack.Spec.Auth = &v1beta1.AuthSpec{
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
				Eventually(conditionStatus(stack, v1beta1.ConditionTypeStackAuthCreated)).Should(Equal(metav1.ConditionTrue))
			})
			It("Should create a auth server on a new namespace", func() {
				Expect(exists(types.NamespacedName{
					Name:      stack.Spec.Auth.Name(stack),
					Namespace: stack.Spec.Namespace,
				}, &componentsv1beta1.Auth{})()).To(BeTrue())
			})
			Context("Then removing auth", func() {
				BeforeEach(func() {
					stack.Spec.Auth = nil
					Expect(k8sClient.Update(ctx, stack)).To(BeNil())
					Eventually(conditionStatus(stack, v1beta1.ConditionTypeStackAuthCreated)).Should(Equal(metav1.ConditionUnknown))
				})
				It("Should remove Auth deployment", func() {
					Expect(exists(types.NamespacedName{
						Name:      stack.Name,
						Namespace: stack.Spec.Namespace,
					}, &componentsv1beta1.Auth{})()).To(BeFalse())
				})
			})
		})
	})
})
