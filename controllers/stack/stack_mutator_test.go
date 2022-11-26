package stack

import (
	"github.com/google/uuid"
	authcomponentsv1beta1 "github.com/numary/operator/apis/auth.components/v1beta1"
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	stackv1beta2 "github.com/numary/operator/apis/stack/v1beta2"
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	. "github.com/numary/operator/pkg/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Stack controller (Auth)", func() {
	mutator := NewMutator(GetClient(), GetScheme(), []string{"*.example.com"})
	WithMutator(mutator, func() {
		When("Creating a stack with no configuration object", func() {
			var (
				stack           *stackv1beta2.Stack
				configurationId string
			)
			BeforeEach(func() {
				name := uuid.NewString()
				configurationId = uuid.NewString()

				stack = &stackv1beta2.Stack{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
					Spec: stackv1beta2.StackSpec{
						Namespace: name,
						Seed:      configurationId,
					},
				}
				Expect(Create(stack)).To(Succeed())
				Eventually(ConditionStatus(stack, apisv1beta2.ConditionTypeError)).
					Should(Equal(metav1.ConditionTrue))
			})
			Context("Then creating the configuration object", func() {
				var (
					configuration *stackv1beta2.Configuration
				)
				BeforeEach(func() {
					configuration = &stackv1beta2.Configuration{
						ObjectMeta: metav1.ObjectMeta{
							Name: configurationId,
						},
					}
					Expect(Create(configuration)).To(Succeed())
				})
				It("Should resolve the error", func() {
					Eventually(ConditionStatus(stack, apisv1beta2.ConditionTypeError)).
						Should(Equal(metav1.ConditionUnknown))
				})
			})
		})
		When("Creating a configuration", func() {
			var (
				configuration *stackv1beta2.Configuration
			)
			BeforeEach(func() {
				configuration = &stackv1beta2.Configuration{
					ObjectMeta: metav1.ObjectMeta{
						Name: uuid.NewString(),
					},
				}
				Expect(Create(configuration)).To(Succeed())
			})
			Context("Then creating a stack", func() {
				var (
					stack *stackv1beta2.Stack
				)
				BeforeEach(func() {
					name := uuid.NewString()

					stack = &stackv1beta2.Stack{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
						},
						Spec: stackv1beta2.StackSpec{
							Namespace: name,
							Seed:      configuration.Name,
							Auth: stackv1beta2.StackAuthSpec{
								DelegatedOIDCServer: componentsv1beta1.DelegatedOIDCServerConfiguration{
									Issuer:       "http://example.net",
									ClientID:     "clientId",
									ClientSecret: "clientSecret",
								},
							},
						},
					}

					Expect(Create(stack)).To(Succeed())
					Eventually(ConditionStatus(stack, apisv1beta2.ConditionTypeReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a new namespace", func() {
					Expect(Get(types.NamespacedName{
						Name: stack.Spec.Namespace,
					}, &v1.Namespace{})).To(BeNil())
				})
				Context("With ingress", func() {
					BeforeEach(func() {
						configuration.Spec.Ingress = &stackv1beta2.IngressGlobalConfig{
							TLS: &apisv1beta2.IngressTLS{
								SecretName: uuid.NewString(),
							},
						}
						Expect(Update(configuration)).To(BeNil())
					})
				})
				Context("With ledger service", func() {
					BeforeEach(func() {
						configuration.Spec.Services.Ledger = &stackv1beta2.LedgerSpec{
							Postgres: apisv1beta2.PostgresConfig{
								Port:     1234,
								Host:     "XXX",
								Username: "XXX",
								Password: "XXX",
							},
						}
						Expect(Update(configuration)).To(BeNil())
						Eventually(ConditionStatus(stack, stackv1beta2.ConditionTypeStackLedgerReady)).
							Should(Equal(metav1.ConditionTrue))
					})
					It("Should create a ledger on a new namespace", func() {
						ledger := &componentsv1beta2.Ledger{
							ObjectMeta: metav1.ObjectMeta{
								Name:      stack.ServiceName("ledger"),
								Namespace: stack.Spec.Namespace,
							},
						}
						Expect(Exists(ledger)()).To(BeTrue())
					})
				})
				Context("With auth and control", func() {
					BeforeEach(func() {
						configuration.Spec.Services.Auth = &stackv1beta2.AuthSpec{
							Postgres: apisv1beta2.PostgresConfig{
								Port:     5432,
								Host:     "postgres",
								Username: "admin",
								Password: "admin",
							},
						}
						configuration.Spec.Services.Control = &stackv1beta2.ControlSpec{}
						Expect(Update(configuration)).To(BeNil())
						Eventually(ConditionStatus(stack, stackv1beta2.ConditionTypeStackAuthReady)).
							Should(Equal(metav1.ConditionTrue))
						Eventually(ConditionStatus(stack, stackv1beta2.ConditionTypeStackControlReady)).
							Should(Equal(metav1.ConditionTrue))
						Eventually(ConditionStatus(stack, apisv1beta2.ConditionTypeReady)).
							Should(Equal(metav1.ConditionTrue))
					})
					It("Should register a static auth client into stack status and use it on control", func() {
						Eventually(func() authcomponentsv1beta1.StaticClient {
							Expect(Get(types.NamespacedName{
								Namespace: stack.Namespace,
								Name:      stack.Name,
							}, stack)).To(Succeed())
							return stack.Status.StaticAuthClients["control"]
						}).ShouldNot(BeZero())
						control := &componentsv1beta2.Control{
							ObjectMeta: metav1.ObjectMeta{
								Name:      stack.ServiceName("control"),
								Namespace: stack.Spec.Namespace,
							},
						}
						Eventually(Exists(control)()).Should(BeTrue())
						Expect(control.Spec.AuthClientConfiguration).NotTo(BeNil())
						Expect(control.Spec.AuthClientConfiguration.ClientID).
							To(Equal(stack.Status.StaticAuthClients["control"].ID))
						Expect(control.Spec.AuthClientConfiguration.ClientSecret).
							To(Equal(stack.Status.StaticAuthClients["control"].Secrets[0]))
					})
				})
				Context("With auth configuration", func() {
					BeforeEach(func() {
						configuration.Spec.Services.Auth = &stackv1beta2.AuthSpec{
							Postgres: apisv1beta2.PostgresConfig{
								Port:     5432,
								Host:     "postgres",
								Username: "admin",
								Password: "admin",
							},
						}
						Expect(Update(configuration)).To(BeNil())
						Eventually(ConditionStatus(stack, stackv1beta2.ConditionTypeStackAuthReady)).
							Should(Equal(metav1.ConditionTrue))
						Eventually(ConditionStatus(stack, apisv1beta2.ConditionTypeReady)).
							Should(Equal(metav1.ConditionTrue))
					})
					It("Should create a auth server on a new namespace", func() {
						Expect(Exists(&componentsv1beta2.Auth{
							ObjectMeta: metav1.ObjectMeta{
								Name:      stack.ServiceName("auth"),
								Namespace: stack.Spec.Namespace,
							},
						})()).To(BeTrue())
					})
					Context("Then removing auth", func() {
						BeforeEach(func() {
							configuration.Spec.Services.Auth = nil
							Expect(Update(configuration)).To(BeNil())
							Eventually(ConditionStatus(stack, stackv1beta2.ConditionTypeStackAuthReady)).Should(Equal(metav1.ConditionUnknown))
						})
						It("Should remove Auth deployment", func() {
							Expect(Exists(&componentsv1beta2.Auth{
								ObjectMeta: metav1.ObjectMeta{
									Name:      stack.Name,
									Namespace: stack.Spec.Namespace,
								},
							})()).To(BeFalse())
						})
					})
				})
			})
		})
	})
})
