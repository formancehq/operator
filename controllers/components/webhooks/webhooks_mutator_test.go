package webhooks

import (
	. "github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func ownerReference(webhooks *Webhooks) metav1.OwnerReference {
	return metav1.OwnerReference{
		Kind:               "Webhooks",
		APIVersion:         "components.formance.com/v1beta1",
		Name:               "webhooks",
		UID:                webhooks.UID,
		BlockOwnerDeletion: pointer.Bool(true),
		Controller:         pointer.Bool(true),
	}
}

var _ = Describe("Webhooks controller", func() {
	mutator := NewMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			Context("When creating a webhooks server", func() {
				var (
					webhooks *Webhooks
				)
				BeforeEach(func() {
					webhooks = &Webhooks{
						ObjectMeta: metav1.ObjectMeta{
							Name: "webhooks",
						},
						Spec: WebhooksSpec{
							Collector: &CollectorConfig{
								KafkaConfig: KafkaConfig{
									Brokers: []string{"http://kafka"},
									TLS:     false,
									SASL:    nil,
								},
								Topic: "xxx",
							},
							MongoDB: MongoDBConfig{
								Host:     "XXX",
								Port:     27017,
								Username: "foo",
								Password: "bar",
								Database: "test",
							},
						},
					}
					Expect(Create(webhooks)).To(BeNil())
					Eventually(ConditionStatus(webhooks, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a deployment", func() {
					Eventually(ConditionStatus(webhooks, ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      webhooks.Name,
							Namespace: webhooks.Namespace,
						},
					}
					Expect(Exists(deployment)()).To(BeTrue())
					Expect(deployment.OwnerReferences).To(HaveLen(1))
					Expect(deployment.OwnerReferences).To(ContainElement(ownerReference(webhooks)))
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(webhooks, ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      webhooks.Name,
							Namespace: webhooks.Namespace,
						},
					}
					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(ownerReference(webhooks)))
				})
				Context("Then enable ingress", func() {
					BeforeEach(func() {
						webhooks.Spec.Ingress = &IngressSpec{
							Path: "/webhooks",
							Host: "localhost",
						}
						Expect(Update(webhooks)).To(BeNil())
					})
					It("Should create a ingress", func() {
						Eventually(ConditionStatus(webhooks, ConditionTypeIngressReady)).Should(Equal(metav1.ConditionTrue))
						ingress := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      webhooks.Name,
								Namespace: webhooks.Namespace,
							},
						}
						Expect(Exists(ingress)()).To(BeTrue())
						Expect(ingress.OwnerReferences).To(HaveLen(1))
						Expect(ingress.OwnerReferences).To(ContainElement(ownerReference(webhooks)))
					})
					Context("Then disabling ingress support", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(webhooks, ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionTrue))
							webhooks.Spec.Ingress = nil
							Expect(Update(webhooks)).To(BeNil())
							Eventually(ConditionStatus(webhooks, ConditionTypeIngressReady)).
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
