package payments

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

func ownerReference(payments *Payments) metav1.OwnerReference {
	return metav1.OwnerReference{
		Kind:               "Payments",
		APIVersion:         "components.formance.com/v1beta1",
		Name:               "payments",
		UID:                payments.UID,
		BlockOwnerDeletion: pointer.Bool(true),
		Controller:         pointer.Bool(true),
	}
}

var _ = Describe("Payments controller", func() {
	mutator := NewMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			Context("When creating a payments server", func() {
				var (
					payments *Payments
				)
				BeforeEach(func() {
					payments = &Payments{
						ObjectMeta: metav1.ObjectMeta{
							Name: "payments",
						},
						Spec: PaymentsSpec{
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
							ElasticSearchIndex: "foo",
						},
					}
					Expect(Create(payments)).To(BeNil())
					Eventually(ConditionStatus(payments, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a deployment", func() {
					Eventually(ConditionStatus(payments, ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      payments.Name,
							Namespace: payments.Namespace,
						},
					}
					Expect(Exists(deployment)()).To(BeTrue())
					Expect(deployment.OwnerReferences).To(HaveLen(1))
					Expect(deployment.OwnerReferences).To(ContainElement(ownerReference(payments)))
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(payments, ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      payments.Name,
							Namespace: payments.Namespace,
						},
					}
					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(ownerReference(payments)))
				})
				Context("Then enable ingress", func() {
					BeforeEach(func() {
						payments.Spec.Ingress = &IngressSpec{
							Path: "/payments",
							Host: "localhost",
						}
						Expect(Update(payments)).To(BeNil())
					})
					It("Should create a ingress", func() {
						Eventually(ConditionStatus(payments, ConditionTypeIngressReady)).Should(Equal(metav1.ConditionTrue))
						ingress := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      payments.Name,
								Namespace: payments.Namespace,
							},
						}
						Expect(Exists(ingress)()).To(BeTrue())
						Expect(ingress.OwnerReferences).To(HaveLen(1))
						Expect(ingress.OwnerReferences).To(ContainElement(ownerReference(payments)))
					})
					Context("Then disabling ingress support", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(payments, ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionTrue))
							payments.Spec.Ingress = nil
							Expect(Update(payments)).To(BeNil())
							Eventually(ConditionStatus(payments, ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionUnknown))
						})
						It("Should remove the ingress", func() {
							Eventually(NotFound(&networkingv1.Ingress{
								ObjectMeta: metav1.ObjectMeta{
									Name:      payments.Name,
									Namespace: payments.Namespace,
								},
							})).Should(BeTrue())
						})
					})
				})
			})
		})
	})
})
