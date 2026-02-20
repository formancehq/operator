package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"

	v1beta1 "github.com/formancehq/operator/api/formance.com/v1beta1"
	core "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	. "github.com/formancehq/operator/internal/tests/internal"
)

var _ = Describe("TransactionsController", func() {
	Context("When creating a Transactions object with worker enabled", func() {
		var (
			stack                 *v1beta1.Stack
			gateway               *v1beta1.Gateway
			auth                  *v1beta1.Auth
			ledger                *v1beta1.Ledger
			payments              *v1beta1.Payments
			transactions          *v1beta1.Transactions
			databaseSettings      *v1beta1.Settings
			brokerDSNSettings     *v1beta1.Settings
			workerEnabledSettings *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			brokerDSNSettings = settings.New(uuid.NewString(), "broker.dsn", "nats://localhost:1234", stack.Name)
			workerEnabledSettings = settings.New(uuid.NewString(), "transactions.worker-enabled", "true", stack.Name)
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
			transactions = &v1beta1.Transactions{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.TransactionsSpec{
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
			Expect(Create(workerEnabledSettings)).To(Succeed())
			Expect(Create(gateway)).To(Succeed())
			Expect(Create(auth)).To(Succeed())
			Expect(Create(ledger)).To(Succeed())
			Expect(Create(payments)).To(Succeed())
			Expect(Create(transactions)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(stack)).To(Succeed())
			Expect(Delete(databaseSettings)).To(Succeed())
			Expect(Delete(brokerDSNSettings)).To(Succeed())
			Expect(Delete(workerEnabledSettings)).To(Succeed())
		})
		It("Should create a single deployment with embedded worker", func() {
			By("Should set the status to ready", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", transactions.Name, transactions)).To(Succeed())
					return transactions.Status.Ready
				}).Should(BeTrue())
			})
			By("Should add an owner reference on the stack", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", transactions.Name, transactions)).To(Succeed())
					reference, err := core.HasOwnerReference(TestContext(), stack, transactions)
					g.Expect(err).To(BeNil())
					return reference
				}).Should(BeTrue())
			})
			By("Should create a single deployment with WORKER_ENABLED=true", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "transactions", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(transactions))
				Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
					core.Env("WORKER_ENABLED", "true"),
				))
			})
			By("Should create a new GatewayHTTPAPI object", func() {
				httpService := &v1beta1.GatewayHTTPAPI{}
				Eventually(func() error {
					return LoadResource("", core.GetObjectName(stack.Name, "transactions"), httpService)
				}).Should(Succeed())
			})
			By("Should create a new AuthClient object", func() {
				authClient := &v1beta1.AuthClient{}
				Eventually(func() error {
					return LoadResource("", core.GetObjectName(stack.Name, "transactions"), authClient)
				}).Should(Succeed())
			})
			By("Should create a new BrokerConsumer object", func() {
				consumer := &v1beta1.BrokerConsumer{}
				Eventually(func() error {
					return LoadResource("", transactions.Name+"-transactions", consumer)
				}).Should(Succeed())
			})
		})
	})

	Context("When creating a Transactions object with worker disabled (default)", func() {
		var (
			stack             *v1beta1.Stack
			gateway           *v1beta1.Gateway
			auth              *v1beta1.Auth
			ledger            *v1beta1.Ledger
			payments          *v1beta1.Payments
			transactions      *v1beta1.Transactions
			databaseSettings  *v1beta1.Settings
			brokerDSNSettings *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			brokerDSNSettings = settings.New(uuid.NewString(), "broker.dsn", "nats://localhost:1234", stack.Name)
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
			transactions = &v1beta1.Transactions{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.TransactionsSpec{
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
			Expect(Create(gateway)).To(Succeed())
			Expect(Create(auth)).To(Succeed())
			Expect(Create(ledger)).To(Succeed())
			Expect(Create(payments)).To(Succeed())
			Expect(Create(transactions)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(stack)).To(Succeed())
			Expect(Delete(databaseSettings)).To(Succeed())
			Expect(Delete(brokerDSNSettings)).To(Succeed())
		})
		It("Should create separate API and worker deployments", func() {
			By("Should set the status to ready", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", transactions.Name, transactions)).To(Succeed())
					return transactions.Status.Ready
				}).Should(BeTrue())
			})
			By("Should create an API deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "transactions", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(transactions))
				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"serve"}))
			})
			By("Should create a worker deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "transactions-worker", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(transactions))
				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"worker"}))
			})
		})
	})
})
