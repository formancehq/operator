package components

import (
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Webhooks controller", func() {
	mutator := NewWebhooksMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			Context("When creating a webhooks server", func() {
				var (
					webhooks *componentsv1beta2.Webhooks
				)
				BeforeEach(func() {
					webhooks = &componentsv1beta2.Webhooks{
						ObjectMeta: metav1.ObjectMeta{
							Name: "webhooks",
						},
						Spec: componentsv1beta2.WebhooksSpec{
							Collector: &componentsv1beta1.CollectorConfig{
								KafkaConfig: NewDumpKafkaConfig(),
								Topic:       "xxx",
							},
							MongoDB: NewDumpMongoDBConfig(),
						},
					}
					Expect(Create(webhooks)).To(BeNil())
					Eventually(ConditionStatus(webhooks, apisv1beta1.ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a deployment", func() {
					Eventually(ConditionStatus(webhooks, apisv1beta1.ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      webhooks.Name,
							Namespace: webhooks.Namespace,
						},
					}
					Expect(Exists(deployment)()).To(BeTrue())
					Expect(deployment.OwnerReferences).To(HaveLen(1))
					Expect(deployment.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(webhooks)))
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(webhooks, apisv1beta1.ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      webhooks.Name,
							Namespace: webhooks.Namespace,
						},
					}
					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(webhooks)))
				})
				Context("Then enable ingress", func() {
					BeforeEach(func() {
						webhooks.Spec.Ingress = &apisv1beta2.IngressSpec{
							Path: "/webhooks",
							Host: "localhost",
						}
						Expect(Update(webhooks)).To(BeNil())
					})
					It("Should create a ingress", func() {
						Eventually(ConditionStatus(webhooks, apisv1beta1.ConditionTypeIngressReady)).Should(Equal(metav1.ConditionTrue))
						ingress := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      webhooks.Name,
								Namespace: webhooks.Namespace,
							},
						}
						Expect(Exists(ingress)()).To(BeTrue())
						Expect(ingress.OwnerReferences).To(HaveLen(1))
						Expect(ingress.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(webhooks)))
					})
					Context("Then disabling ingress support", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(webhooks, apisv1beta1.ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionTrue))
							webhooks.Spec.Ingress = nil
							Expect(Update(webhooks)).To(BeNil())
							Eventually(ConditionStatus(webhooks, apisv1beta1.ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionUnknown))
						})
						It("Should remove the ingress", func() {
							Eventually(NotFound(&networkingv1.Ingress{
								ObjectMeta: metav1.ObjectMeta{
									Name:      webhooks.Name,
									Namespace: webhooks.Namespace,
								},
							})).Should(BeTrue())
						})
					})
				})
			})
		})
	})
})