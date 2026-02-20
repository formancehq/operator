package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/resources/settings"
	. "github.com/formancehq/operator/internal/tests/internal"
)

var _ = Describe("NetworkPolicyController", func() {
	Context("When creating a Stack", func() {
		var (
			stack *v1beta1.Stack
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(BeNil())
		})
		AfterEach(func() {
			Expect(Delete(stack)).To(BeNil())
		})

		It("Should not create NetworkPolicies by default", func() {
			Consistently(func() int {
				npList := &networkingv1.NetworkPolicyList{}
				Expect(List(npList, client.InNamespace(stack.Name))).To(Succeed())
				return len(npList.Items)
			}).Should(Equal(0))
		})

		Context("With networkpolicies.enabled=true", func() {
			var (
				enabledSetting *v1beta1.Settings
			)
			JustBeforeEach(func() {
				enabledSetting = settings.New(uuid.NewString(), "networkpolicies.enabled", "true", stack.Name)
				Expect(Create(enabledSetting)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(enabledSetting)).To(Succeed())
			})

			It("Should create 3 NetworkPolicies controlled by the stack", func() {
				// Check default-deny-ingress
				Eventually(func(g Gomega) {
					np := &networkingv1.NetworkPolicy{}
					g.Expect(LoadResource(stack.Name, "default-deny-ingress", np)).To(Succeed())
					g.Expect(np).To(BeControlledBy(stack))
					g.Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
					g.Expect(np.Spec.Ingress).To(BeEmpty())
				}).Should(Succeed())

				// Check allow-gateway-ingress
				Eventually(func(g Gomega) {
					np := &networkingv1.NetworkPolicy{}
					g.Expect(LoadResource(stack.Name, "allow-gateway-ingress", np)).To(Succeed())
					g.Expect(np).To(BeControlledBy(stack))
					g.Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/name", "gateway"))
					g.Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
					g.Expect(np.Spec.Ingress).To(HaveLen(1))
				}).Should(Succeed())

				// Check allow-from-gateway
				Eventually(func(g Gomega) {
					np := &networkingv1.NetworkPolicy{}
					g.Expect(LoadResource(stack.Name, "allow-from-gateway", np)).To(Succeed())
					g.Expect(np).To(BeControlledBy(stack))
					g.Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
					g.Expect(np.Spec.Ingress).To(HaveLen(1))
					g.Expect(np.Spec.Ingress[0].From).To(HaveLen(1))
					g.Expect(np.Spec.Ingress[0].From[0].PodSelector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/name", "gateway"))
				}).Should(Succeed())
			})

			Context("Then disabling networkpolicies", func() {
				JustBeforeEach(func() {
					// Wait for policies to be created first
					Eventually(func() error {
						return LoadResource(stack.Name, "default-deny-ingress", &networkingv1.NetworkPolicy{})
					}).Should(Succeed())

					patch := client.MergeFrom(enabledSetting.DeepCopy())
					enabledSetting.Spec.Value = "false"
					Expect(Patch(enabledSetting, patch)).To(Succeed())
				})

				It("Should delete all NetworkPolicies", func() {
					Eventually(func() error {
						return LoadResource(stack.Name, "default-deny-ingress", &networkingv1.NetworkPolicy{})
					}).Should(BeNotFound())
					Eventually(func() error {
						return LoadResource(stack.Name, "allow-gateway-ingress", &networkingv1.NetworkPolicy{})
					}).Should(BeNotFound())
					Eventually(func() error {
						return LoadResource(stack.Name, "allow-from-gateway", &networkingv1.NetworkPolicy{})
					}).Should(BeNotFound())
				})
			})
		})

		Context("With wildcard networkpolicies.enabled=true", func() {
			var (
				enabledSetting *v1beta1.Settings
			)
			JustBeforeEach(func() {
				enabledSetting = settings.New(uuid.NewString(), "networkpolicies.enabled", "true", "*")
				Expect(Create(enabledSetting)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(enabledSetting)).To(Succeed())
			})

			It("Should create NetworkPolicies for the stack", func() {
				Eventually(func(g Gomega) {
					np := &networkingv1.NetworkPolicy{}
					g.Expect(LoadResource(stack.Name, "default-deny-ingress", np)).To(Succeed())
					g.Expect(np).To(BeControlledBy(stack))
				}).Should(Succeed())
			})
		})
	})
})
