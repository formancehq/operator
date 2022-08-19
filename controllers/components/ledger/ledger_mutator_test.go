package ledger

import (
	. "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func ownerReference(ledger *Ledger) metav1.OwnerReference {
	return metav1.OwnerReference{
		Kind:               "Ledger",
		APIVersion:         "components.formance.com/v1beta1",
		Name:               "ledger",
		UID:                ledger.UID,
		BlockOwnerDeletion: pointer.Bool(true),
		Controller:         pointer.Bool(true),
	}
}

var _ = Describe("Ledger controller", func() {
	mutator := NewMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			Context("When creating a ledger server", func() {
				var (
					ledger *Ledger
				)
				BeforeEach(func() {
					ledger = &Ledger{
						ObjectMeta: metav1.ObjectMeta{
							Name: "ledger",
						},
						Spec: LedgerSpec{
							Postgres: PostgresConfigCreateDatabase{
								PostgresConfig: PostgresConfig{
									Database: "ledger",
									Port:     5432,
									Host:     "postgres",
									Username: "ledger",
									Password: "ledger",
								},
								CreateDatabase: true,
							},
							Kafka: &KafkaConfig{
								Brokers: []string{"http://kafka"},
								TLS:     false,
								SASL:    nil,
							},
						},
					}
					Expect(Create(ledger)).To(BeNil())
					Eventually(ConditionStatus(ledger, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a deployment", func() {
					Eventually(ConditionStatus(ledger, ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      ledger.Name,
							Namespace: ledger.Namespace,
						},
					}
					Expect(Exists(deployment)()).To(BeTrue())
					Expect(deployment.OwnerReferences).To(HaveLen(1))
					Expect(deployment.OwnerReferences).To(ContainElement(ownerReference(ledger)))
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(ledger, ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      ledger.Name,
							Namespace: ledger.Namespace,
						},
					}
					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(ownerReference(ledger)))
				})
				Context("Then enable ingress", func() {
					BeforeEach(func() {
						ledger.Spec.Ingress = &IngressSpec{
							Path: "/ledger",
							Host: "localhost",
						}
						Expect(Update(ledger)).To(BeNil())
					})
					It("Should create a ingress", func() {
						Eventually(ConditionStatus(ledger, ConditionTypeIngressReady)).Should(Equal(metav1.ConditionTrue))
						ingress := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      ledger.Name,
								Namespace: ledger.Namespace,
							},
						}
						Expect(Exists(ingress)()).To(BeTrue())
						Expect(ingress.OwnerReferences).To(HaveLen(1))
						Expect(ingress.OwnerReferences).To(ContainElement(ownerReference(ledger)))
					})
					Context("Then disabling ingress support", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(ledger, ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionTrue))
							ledger.Spec.Ingress = nil
							Expect(Update(ledger)).To(BeNil())
							Eventually(ConditionStatus(ledger, ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionUnknown))
						})
						It("Should remove the ingress", func() {
							Eventually(NotFound(&networkingv1.Ingress{
								ObjectMeta: metav1.ObjectMeta{
									Name:      ledger.Name,
									Namespace: ledger.Namespace,
								},
							})).Should(BeTrue())
						})
					})
				})
			})
		})
	})
})
