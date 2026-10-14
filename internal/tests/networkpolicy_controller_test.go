package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("NetworkPolicyController", func() {
	Context("When creating a Stack", func() {
		var stack *v1beta1.Stack
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
		})
		JustBeforeEach(func() { Expect(Create(stack)).To(Succeed()) })
		AfterEach(func() { Expect(Delete(stack)).To(Succeed()) })

		It("Should not create NetworkPolicies by default", func() {
			Consistently(func() int {
				npList := &networkingv1.NetworkPolicyList{}
				Expect(List(npList, client.InNamespace(stack.Name))).To(Succeed())
				return len(npList.Items)
			}).Should(Equal(0))
		})

		Context("With networkpolicies.enabled=true", func() {
			var enabledSetting *v1beta1.Settings
			JustBeforeEach(func() {
				enabledSetting = settings.New(uuid.NewString(), "networkpolicies.enabled", "true", stack.Name)
				Expect(Create(enabledSetting)).To(Succeed())
			})
			AfterEach(func() {
				// The Settings deletion test has already removed this resource.
				if enabledSetting != nil {
					Expect(Delete(enabledSetting)).To(Succeed())
				}
			})

			It("Should create exactly two ingress policies controlled by the stack", func() {
				Eventually(func(g Gomega) {
					npList := &networkingv1.NetworkPolicyList{}
					g.Expect(List(npList, client.InNamespace(stack.Name))).To(Succeed())
					g.Expect(npList.Items).To(HaveLen(2))

					local := &networkingv1.NetworkPolicy{}
					g.Expect(LoadResource(stack.Name, "allow-same-namespace", local)).To(Succeed())
					g.Expect(local).To(BeControlledBy(stack))
					g.Expect(local.Spec.PodSelector).To(Equal(metav1.LabelSelector{}))
					g.Expect(local.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
					g.Expect(local.Spec.Egress).To(BeEmpty())
					g.Expect(local.Spec.Ingress).To(HaveLen(1))
					g.Expect(local.Spec.Ingress[0].Ports).To(BeEmpty())
					g.Expect(local.Spec.Ingress[0].From).To(Equal([]networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{}},
					}))

					gateway := &networkingv1.NetworkPolicy{}
					g.Expect(LoadResource(stack.Name, "allow-gateway-ingress", gateway)).To(Succeed())
					g.Expect(gateway).To(BeControlledBy(stack))
					g.Expect(gateway.Spec.PodSelector).To(Equal(metav1.LabelSelector{
						MatchLabels: map[string]string{"app.kubernetes.io/name": "gateway"},
					}))
					g.Expect(gateway.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
					g.Expect(gateway.Spec.Egress).To(BeEmpty())
					g.Expect(gateway.Spec.Ingress).To(HaveLen(1))
					g.Expect(gateway.Spec.Ingress[0].From).To(BeEmpty())
					g.Expect(gateway.Spec.Ingress[0].Ports).To(HaveLen(1))
					protocol := corev1.ProtocolTCP
					g.Expect(gateway.Spec.Ingress[0].Ports[0].Protocol).To(Equal(&protocol))
					g.Expect(gateway.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(8080))
				}).Should(Succeed())
			})

			It("Should delete the policies when the setting is disabled", func() {
				Eventually(func() error {
					return LoadResource(stack.Name, "allow-same-namespace", &networkingv1.NetworkPolicy{})
				}).Should(Succeed())
				Eventually(func() error {
					return LoadResource(stack.Name, "allow-gateway-ingress", &networkingv1.NetworkPolicy{})
				}).Should(Succeed())

				patch := client.MergeFrom(enabledSetting.DeepCopy())
				enabledSetting.Spec.Value = "false"
				Expect(Patch(enabledSetting, patch)).To(Succeed())

				Eventually(func(g Gomega) {
					npList := &networkingv1.NetworkPolicyList{}
					g.Expect(List(npList, client.InNamespace(stack.Name))).To(Succeed())
					g.Expect(npList.Items).To(BeEmpty())
				}).Should(Succeed())
			})

			It("Should delete the policies when the setting is removed", func() {
				Eventually(func() error {
					return LoadResource(stack.Name, "allow-same-namespace", &networkingv1.NetworkPolicy{})
				}).Should(Succeed())
				Eventually(func() error {
					return LoadResource(stack.Name, "allow-gateway-ingress", &networkingv1.NetworkPolicy{})
				}).Should(Succeed())

				Expect(Delete(enabledSetting)).To(Succeed())
				enabledSetting = nil

				Eventually(func(g Gomega) {
					npList := &networkingv1.NetworkPolicyList{}
					g.Expect(List(npList, client.InNamespace(stack.Name))).To(Succeed())
					g.Expect(npList.Items).To(BeEmpty())
				}).Should(Succeed())
			})
		})

		Context("With wildcard networkpolicies.enabled=true", func() {
			var enabledSetting *v1beta1.Settings
			JustBeforeEach(func() {
				enabledSetting = settings.New(uuid.NewString(), "networkpolicies.enabled", "true", "*")
				Expect(Create(enabledSetting)).To(Succeed())
			})
			AfterEach(func() { Expect(Delete(enabledSetting)).To(Succeed()) })

			It("Should create both NetworkPolicies for the stack", func() {
				Eventually(func(g Gomega) {
					for _, name := range []string{"allow-same-namespace", "allow-gateway-ingress"} {
						np := &networkingv1.NetworkPolicy{}
						g.Expect(LoadResource(stack.Name, name, np)).To(Succeed())
						g.Expect(np).To(BeControlledBy(stack))
					}
				}).Should(Succeed())
			})
		})
	})
})
