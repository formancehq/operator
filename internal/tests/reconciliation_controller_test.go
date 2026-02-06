package tests_test

import (
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"

	v1beta1 "github.com/formancehq/operator/api/formance.com/v1beta1"
	core "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	. "github.com/formancehq/operator/internal/tests/internal"
)

var _ = Describe("ReconciliationController", func() {
	Context("When creating a Reconciliation object", func() {
		var (
			stack                 *v1beta1.Stack
			gateway               *v1beta1.Gateway
			auth                  *v1beta1.Auth
			ledger                *v1beta1.Ledger
			payments              *v1beta1.Payments
			reconciliation        *v1beta1.Reconciliation
			databaseSettings      *v1beta1.Settings
			brokerDSNSettings     *v1beta1.Settings
			elasticsearchSettings *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			brokerDSNSettings = settings.New(uuid.NewString(), "broker.dsn", "nats://localhost:1234", stack.Name)
			elasticsearchSettings = settings.New(uuid.NewString(), "elasticsearch.dsn", "https://elasticsearch:9200?username=elastic&password=changeme&ilmEnabled=true&ilmHotPhaseDays=30", stack.Name)
			gateway = &v1beta1.Gateway{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.GatewaySpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
					Ingress: &v1beta1.GatewayIngress{},
				},
			}
			auth = &v1beta1.Auth{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.AuthSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
			ledger = &v1beta1.Ledger{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
			payments = &v1beta1.Payments{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.PaymentsSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
			reconciliation = &v1beta1.Reconciliation{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.ReconciliationSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(databaseSettings)).To(Succeed())
			Expect(Create(brokerDSNSettings)).To(BeNil())
			Expect(Create(elasticsearchSettings)).To(Succeed())
			Expect(Create(gateway)).To(Succeed())
			Expect(Create(auth)).To(Succeed())
			Expect(Create(ledger)).To(Succeed())
			Expect(Create(payments)).To(Succeed())
			Expect(Create(reconciliation)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(stack)).To(Succeed())
			Expect(Delete(databaseSettings)).To(Succeed())
			Expect(Delete(brokerDSNSettings)).To(Succeed())
			Expect(Delete(elasticsearchSettings)).To(Succeed())
		})
		It("Should create appropriate components", func() {
			By("Should set the status to ready", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", reconciliation.Name, reconciliation)).To(Succeed())
					return reconciliation.Status.Ready
				}).Should(BeTrue())
			})
			By("Should add an owner reference on the stack", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", reconciliation.Name, reconciliation)).To(Succeed())
					reference, err := core.HasOwnerReference(TestContext(), stack, reconciliation)
					g.Expect(err).To(BeNil())
					return reference
				}).Should(BeTrue())
			})
			By("Should create an API deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "reconciliation", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(reconciliation))
				Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("reconciliation"))
				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"serve"}))
			})
			By("Should create a worker deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "reconciliation-worker", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(reconciliation))
				Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("reconciliation-worker"))
				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"worker"}))
			})
			By("API deployment should have broker environment variables", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "reconciliation", deployment)
				}).Should(Succeed())
				Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
					core.Env("BROKER", "nats"),
					core.Env("PUBLISHER_NATS_ENABLED", "true"),
				))
			})
			By("Worker deployment should have broker environment variables", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "reconciliation-worker", deployment)
				}).Should(Succeed())
				Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
					core.Env("BROKER", "nats"),
					core.Env("PUBLISHER_NATS_ENABLED", "true"),
				))
			})
			By("Worker deployment should have topics environment variable", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "reconciliation-worker", deployment)
				}).Should(Succeed())
				Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
					core.Env("KAFKA_TOPICS", fmt.Sprintf("%s.ledger %s.payments", stack.Name, stack.Name)),
				))
			})
			By("API deployment should NOT have topics environment variable", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "reconciliation", deployment)
				}).Should(Succeed())
				for _, envVar := range deployment.Spec.Template.Spec.Containers[0].Env {
					Expect(envVar.Name).NotTo(Equal("KAFKA_TOPICS"))
				}
			})
			By("Should create a new GatewayHTTPAPI object", func() {
				httpService := &v1beta1.GatewayHTTPAPI{}
				Eventually(func() error {
					return LoadResource("", core.GetObjectName(stack.Name, "reconciliation"), httpService)
				}).Should(Succeed())
			})
			By("Should create a new AuthClient object", func() {
				authClient := &v1beta1.AuthClient{}
				Eventually(func() error {
					return LoadResource("", core.GetObjectName(stack.Name, "reconciliation"), authClient)
				}).Should(Succeed())
			})
			By("Should create a new BrokerConsumer object", func() {
				consumer := &v1beta1.BrokerConsumer{}
				Eventually(func() error {
					return LoadResource("", reconciliation.Name+"-reconciliation", consumer)
				}).Should(Succeed())
			})
			By("BrokerConsumer should have correct services", func() {
				consumer := &v1beta1.BrokerConsumer{}
				Eventually(func(g Gomega) []string {
					g.Expect(LoadResource("", reconciliation.Name+"-reconciliation", consumer)).To(Succeed())
					return consumer.Spec.Services
				}).Should(ContainElements("ledger", "payments"))
			})
			By("Worker deployment should have Elasticsearch environment variables", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "reconciliation-worker", deployment)
				}).Should(Succeed())
				Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
					core.Env("ELASTICSEARCH_URL", "https://elasticsearch:9200"),
					core.Env("ELASTICSEARCH_USERNAME", "elastic"),
					core.Env("ELASTICSEARCH_PASSWORD", "changeme"),
					core.Env("ELASTICSEARCH_ILM_ENABLED", "true"),
					core.Env("ELASTICSEARCH_ILM_HOT_PHASE_DAYS", "30"),
					core.Env("ELASTICSEARCH_ILM_WARM_PHASE_ROLLOVER_DAYS", "365"),
					core.Env("ELASTICSEARCH_ILM_DELETE_PHASE_ENABLED", "false"),
					core.Env("ELASTICSEARCH_ILM_DELETE_PHASE_DAYS", "0"),
				))
			})
			By("API deployment should NOT have Elasticsearch environment variables", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "reconciliation", deployment)
				}).Should(Succeed())
				for _, envVar := range deployment.Spec.Template.Spec.Containers[0].Env {
					Expect(envVar.Name).NotTo(Equal("ELASTICSEARCH_URL"))
				}
			})
		})
	})
})
