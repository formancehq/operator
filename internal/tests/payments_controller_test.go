package tests_test

import (
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("PaymentsController", func() {
	Context("When creating a Payments object", func() {
		var (
			stack               *v1beta1.Stack
			payments            *v1beta1.Payments
			databaseSettings    *v1beta1.Settings
			temporalDSNSettings *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
			payments = &v1beta1.Payments{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.PaymentsSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			temporalDSNSettings = settings.New(uuid.NewString(), "temporal.dsn", "temporal://localhost/namespace", stack.Name)
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(databaseSettings)).To(Succeed())
			Expect(Create(temporalDSNSettings)).To(Succeed())
			Expect(Create(payments)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(payments)).To(Succeed())
			Expect(Delete(databaseSettings)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})
		It("Should create resources", func() {
			By("Should add an owner reference on the stack", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", payments.Name, payments)).To(Succeed())
					reference, err := core.HasOwnerReference(TestContext(), stack, payments)
					g.Expect(err).To(BeNil())
					return reference
				}).Should(BeTrue())
			})
			By("Should create a worker deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "payments-worker", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(payments))
			})
			By("Should create an api deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "payments", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(payments))
			})
		})
		Context("using secrets for temporal", func() {
			var (
				tlsSecret           *corev1.Secret
				encryptionKeySecret *corev1.Secret
				panicsBefore        float64
			)
			BeforeEach(func() {
				panicsBefore = reconcilePanics("payments")
				tlsSecret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      uuid.NewString(),
						Namespace: "default",
						Labels: map[string]string{
							v1beta1.StackLabel: stack.Name,
						},
						Annotations: map[string]string{
							resourcereferences.RewrittenResourceName: "temporal-tls",
						},
					},
					Data: map[string][]byte{
						"tls.crt": []byte("crt"),
						"tls.key": []byte("key"),
					},
				}
				encryptionKeySecret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      uuid.NewString(),
						Namespace: "default",
						Labels: map[string]string{
							v1beta1.StackLabel: stack.Name,
						},
						Annotations: map[string]string{
							resourcereferences.RewrittenResourceName: "temporal-encryption-key",
						},
					},
					Data: map[string][]byte{
						"key": []byte("key"),
					},
				}
				Expect(Create(tlsSecret)).To(Succeed())
				Expect(Create(encryptionKeySecret)).To(Succeed())
				temporalDSNSettings.Spec.Value = "temporal://localhost/namespace?secret=temporal-tls&encryptionKeySecret=temporal-encryption-key"
			})
			AfterEach(func() {
				Expect(Delete(tlsSecret)).To(Succeed())
				Expect(Delete(encryptionKeySecret)).To(Succeed())
			})
			It("Should replicate the secrets and annotate deployments with their hashes", func() {
				for _, name := range []string{"payments-temporal", "payments-temporal-encryption-key"} {
					resourceReference := &v1beta1.ResourceReference{}
					Eventually(func(g Gomega) {
						g.Expect(LoadResource("", fmt.Sprintf("%s-%s", payments.Name, name), resourceReference)).To(Succeed())
						g.Expect(resourceReference.Status.Ready).To(BeTrue())
					}).Should(Succeed())
					Expect(resourceReference).To(BeControlledBy(payments))

					for _, deploymentName := range []string{"payments", "payments-worker"} {
						deployment := &appsv1.Deployment{}
						Eventually(func(g Gomega) {
							g.Expect(LoadResource(stack.Name, deploymentName, deployment)).To(Succeed())
							g.Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(name, resourceReference.Status.Hash))
						}).Should(Succeed())
					}
				}
				// controller-runtime recovers reconcile panics and retries, so a panic
				// while the references are pending would otherwise go unnoticed.
				Expect(reconcilePanics("payments")).To(Equal(panicsBefore))
			})
		})
	})
})

func reconcilePanics(controller string) float64 {
	families, err := metrics.Registry.Gather()
	Expect(err).To(Succeed())
	for _, family := range families {
		if family.GetName() != "controller_runtime_reconcile_panics_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "controller" && label.GetValue() == controller {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}
