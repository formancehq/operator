package stack

import (
	"github.com/google/uuid"
	"github.com/numary/operator/apis/components/auth/v1beta1"
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/apis/stack/v1beta1"
	. "github.com/numary/operator/internal/testing"
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
				stack           *Stack
				configurationId string
			)
			BeforeEach(func() {
				name := uuid.NewString()
				configurationId = uuid.NewString()

				stack = &Stack{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
					Spec: StackSpec{
						Namespace: name,
						Seed:      configurationId,
					},
				}
				Expect(Create(stack)).To(Succeed())
				Eventually(ConditionStatus(stack, ConditionTypeError)).
					Should(Equal(metav1.ConditionTrue))
			})
			Context("Then creating the configuration object", func() {
				var (
					configuration *Configuration
				)
				BeforeEach(func() {
					configuration = &Configuration{
						ObjectMeta: metav1.ObjectMeta{
							Name: configurationId,
						},
					}
					Expect(Create(configuration)).To(Succeed())
				})
				It("Should resolve the error", func() {
					Eventually(ConditionStatus(stack, ConditionTypeError)).
						Should(Equal(metav1.ConditionUnknown))
				})
			})
		})
		When("Creating a configuration", func() {
			var (
				configuration *Configuration
			)
			BeforeEach(func() {
				configuration = &Configuration{
					ObjectMeta: metav1.ObjectMeta{
						Name: uuid.NewString(),
					},
				}
				Expect(Create(configuration)).To(Succeed())
			})
			Context("Then creating a stack", func() {
				var (
					stack *Stack
				)
				BeforeEach(func() {
					name := uuid.NewString()

					stack = &Stack{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
						},
						Spec: StackSpec{
							Namespace: name,
							Seed:      configuration.Name,
						},
					}

					Expect(Create(stack)).To(Succeed())
					Eventually(ConditionStatus(stack, ConditionTypeReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a new namespace", func() {
					Expect(Get(types.NamespacedName{
						Name: stack.Spec.Namespace,
					}, &v1.Namespace{})).To(BeNil())
				})
				Context("With ingress", func() {
					BeforeEach(func() {
						configuration.Spec.Ingress = IngressGlobalConfig{
							TLS: &IngressTLS{
								SecretName: uuid.NewString(),
							},
							Enabled: true,
						}
						Expect(Update(configuration)).To(BeNil())
					})
				})
				Context("With ledger service", func() {
					BeforeEach(func() {
						configuration.Spec.Services.Ledger = &LedgerSpec{
							Postgres: PostgresConfig{
								Port:     1234,
								Host:     "XXX",
								Username: "XXX",
								Password: "XXX",
							},
						}
						Expect(Update(configuration)).To(BeNil())
						Eventually(ConditionStatus(stack, ConditionTypeStackLedgerReady)).
							Should(Equal(metav1.ConditionTrue))
					})
					It("Should create a ledger on a new namespace", func() {
						ledger := &componentsv1beta1.Ledger{
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
						configuration.Spec.Auth = &AuthSpec{
							Postgres: PostgresConfig{
								Port:     5432,
								Host:     "postgres",
								Username: "admin",
								Password: "admin",
							},
							SigningKey: "XXX",
							DelegatedOIDCServer: &componentsv1beta1.DelegatedOIDCServerConfiguration{
								Issuer:       "http://example.net",
								ClientID:     "clientId",
								ClientSecret: "clientSecret",
							},
						}
						configuration.Spec.Services.Control = &ControlSpec{}
						Expect(Update(configuration)).To(BeNil())
						Eventually(ConditionStatus(stack, ConditionTypeStackAuthReady)).
							Should(Equal(metav1.ConditionTrue))
						Eventually(ConditionStatus(stack, ConditionTypeStackControlReady)).
							Should(Equal(metav1.ConditionTrue))
						Eventually(ConditionStatus(stack, ConditionTypeReady)).
							Should(Equal(metav1.ConditionTrue))
					})
					It("Should register a static auth client into stack status and use it on control", func() {
						Eventually(func() v1beta1.StaticClient {
							Expect(Get(types.NamespacedName{
								Namespace: stack.Namespace,
								Name:      stack.Name,
							}, stack)).To(Succeed())
							return stack.Status.StaticAuthClients["control"]
						}).ShouldNot(BeZero())
						control := &componentsv1beta1.Control{
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
						configuration.Spec.Auth = &AuthSpec{
							Postgres: PostgresConfig{
								Port:     5432,
								Host:     "postgres",
								Username: "admin",
								Password: "admin",
							},
							SigningKey: "XXX",
							DelegatedOIDCServer: &componentsv1beta1.DelegatedOIDCServerConfiguration{
								Issuer:       "http://example.net",
								ClientID:     "clientId",
								ClientSecret: "clientSecret",
							},
						}
						Expect(Update(configuration)).To(BeNil())
						Eventually(ConditionStatus(stack, ConditionTypeStackAuthReady)).
							Should(Equal(metav1.ConditionTrue))
						Eventually(ConditionStatus(stack, ConditionTypeReady)).
							Should(Equal(metav1.ConditionTrue))
					})
					It("Should create a auth server on a new namespace", func() {
						Expect(Exists(&componentsv1beta1.Auth{
							ObjectMeta: metav1.ObjectMeta{
								Name:      stack.ServiceName("auth"),
								Namespace: stack.Spec.Namespace,
							},
						})()).To(BeTrue())
					})
					Context("Then removing auth", func() {
						BeforeEach(func() {
							configuration.Spec.Auth = nil
							Expect(Update(configuration)).To(BeNil())
							Eventually(ConditionStatus(stack, ConditionTypeStackAuthReady)).Should(Equal(metav1.ConditionUnknown))
						})
						It("Should remove Auth deployment", func() {
							Expect(Exists(&componentsv1beta1.Auth{
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
