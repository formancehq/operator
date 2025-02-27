package tests_test

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	. "github.com/formancehq/operator/internal/tests/internal"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("AuthController", func() {
	Context("When creating a Auth object", func() {
		var (
			stack            *v1beta1.Stack
			auth             *v1beta1.Auth
			databaseSettings *v1beta1.Settings
			pdbSettings      *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			pdbSettings = settings.New(uuid.NewString(), "deployments.*.pod-disruption-budget", "minAvailable=1", stack.Name)
			auth = &v1beta1.Auth{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.AuthSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(databaseSettings)).To(Succeed())
			Expect(Create(pdbSettings)).To(Succeed())
			Expect(Create(auth)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(databaseSettings)).To(Succeed())
			Expect(Delete(pdbSettings)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})
		It("Should create resources", func() {
			By("Should create a deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "auth", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(auth))
				Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
					core.Env("BASE_URL", "http://auth:8080"),
				))
			})
			By("Should create a pdb", func() {
				pdb := &v1.PodDisruptionBudget{}
				Eventually(func() error {
					return LoadResource(stack.Name, "auth", pdb)
				}).Should(Succeed())
				Expect(pdb).To(BeControlledBy(auth))
				Expect(pdb.Spec.MinAvailable).NotTo(BeNil())
				Expect(*pdb.Spec.MinAvailable).To(Equal(intstr.FromInt32(1)))
			})
			By("Should create a new GatewayHTTPAPI object", func() {
				httpService := &v1beta1.GatewayHTTPAPI{}
				Eventually(func() error {
					return LoadResource("", core.GetObjectName(stack.Name, "auth"), httpService)
				}).Should(Succeed())
			})
			By("Should set the status to ready", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", auth.Name, auth)).To(Succeed())
					return auth.Status.Ready
				}).Should(BeTrue())
			})
			By("Should add an owner reference on the stack", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", auth.Name, auth)).To(Succeed())
					reference, err := core.HasOwnerReference(TestContext(), stack, auth)
					g.Expect(err).To(BeNil())
					return reference
				}).Should(BeTrue())
			})
		})
		Context("Then when disabling the stack", func() {
			JustBeforeEach(func() {
				Eventually(func(g Gomega) *v1beta1.Auth {
					g.Expect(LoadResource("", auth.Name, auth)).To(Succeed())
					return auth
				}).Should(BeReady())
				patch := client.MergeFrom(stack.DeepCopy())
				stack.Spec.Disabled = true
				Expect(Patch(stack, patch)).To(Succeed())
			})
			It("Should remove all dependents objects except the Database object", func() {
				By("It should remove the deployment", func() {
					deployment := &appsv1.Deployment{}
					Eventually(func() error {
						return LoadResource(stack.Name, "auth", deployment)
					}).Should(BeNotFound())
				})
				By("It should remove the GatewayHTTPAPI object", func() {
					gatewayHTTPApi := &v1beta1.GatewayHTTPAPI{}
					Eventually(func() error {
						return LoadResource("", core.GetObjectName(stack.Name, "auth"), gatewayHTTPApi)
					}).Should(BeNotFound())
				})
			})
		})
		When("Creating an AuthClient object", func() {
			var (
				authClient *v1beta1.AuthClient
			)
			JustBeforeEach(func() {
				authClient = &v1beta1.AuthClient{
					ObjectMeta: RandObjectMeta(),
					Spec: v1beta1.AuthClientSpec{
						ID: "client0",
						StackDependency: v1beta1.StackDependency{
							Stack: stack.Name,
						},
						Secret: "secret",
					},
				}
				Expect(Create(authClient)).To(Succeed())
				Eventually(func(g Gomega) []string {
					g.Expect(LoadResource("", auth.Name, auth)).To(Succeed())
					return auth.Status.Clients
				}).Should(ContainElements(authClient.Name))
			})
			JustAfterEach(func() {
				Expect(Delete(authClient)).To(Succeed())
			})
			It("Should configure the config map with the auth client", func() {
				cm := &corev1.ConfigMap{}
				Expect(LoadResource(stack.Name, "auth-configuration", cm)).To(Succeed())
				Expect(cm.Data["config.yaml"]).To(MatchGoldenFile("auth-controller", "config-with-auth-client.yaml"))
			})
			It("Should add an annotations on auth pod deployment template", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) map[string]string {
					g.Expect(LoadResource(stack.Name, "auth", deployment)).To(Succeed())
					return deployment.Spec.Template.Annotations
				}).Should(HaveKey("config-hash"))

			})
		})
		When("Creating an AuthClient object with a secret from a secret", func() {
			var (
				authClient *v1beta1.AuthClient
				secret     *corev1.Secret
			)
			JustBeforeEach(func() {
				Eventually(func() error {
					return LoadResource("", stack.Name, &corev1.Namespace{})
				}).Should(Succeed())

				secret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: uuid.NewString()[:8],
						Labels: map[string]string{
							"formance.com/stack": stack.Name,
						},
					},
					StringData: map[string]string{
						"secret": uuid.NewString(),
					},
				}
				secret.Namespace = stack.Name
				authClient = &v1beta1.AuthClient{
					ObjectMeta: metav1.ObjectMeta{
						Name: uuid.NewString()[:8],
					},
					Spec: v1beta1.AuthClientSpec{
						StackDependency: v1beta1.StackDependency{
							Stack: stack.Name,
						},
						ID: "client0",
						SecretFromSecret: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secret.Name,
							},
							Key: "secret",
						},
					},
				}
				Expect(Create(secret)).To(Succeed())
				Expect(Create(authClient)).To(Succeed())
				Eventually(func(g Gomega) []string {
					g.Expect(LoadResource("", auth.Name, auth)).To(Succeed())
					return auth.Status.Clients
				}).Should(ContainElements(authClient.Name))
			})
			JustAfterEach(func() {
				Expect(Delete(authClient)).To(Succeed())
				Expect(Delete(secret)).To(Succeed())
			})
			It("Should configure the config map with the auth client resource references", func() {
				cm := &corev1.ConfigMap{}
				Expect(LoadResource(stack.Name, "auth-configuration", cm)).To(Succeed())
				envVarName := "AUTH_CLIENT_SECRET_" + strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(authClient.Name, "-", "_"), ".", "_"))
				Expect(cm.Data["config.yaml"]).To(MatchYAML(fmt.Sprintf(`
clients:
    - id: client0
      public: false
      redirectUris: []
      postLogoutRedirectUris: []
      scopes: []
      secret: $%s
      secrets:
        - $%s
`, envVarName, envVarName)))
			})
			It("Should add an annotations on auth pod deployment template", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) map[string]string {
					g.Expect(LoadResource(stack.Name, "auth", deployment)).To(Succeed())
					return deployment.Spec.Template.Annotations
				}).Should(HaveKey("auth-clients-secrets"))
				Expect(deployment.Spec.Template.Annotations["auth-clients-secrets"]).ToNot(BeEmpty())
			})
		})
		Context("with a Gateway", func() {
			var (
				gateway *v1beta1.Gateway
			)
			BeforeEach(func() {
				gateway = &v1beta1.Gateway{
					ObjectMeta: RandObjectMeta(),
					Spec: v1beta1.GatewaySpec{
						StackDependency: v1beta1.StackDependency{
							Stack: stack.Name,
						},
					},
				}
			})
			JustBeforeEach(func() {
				Expect(Create(gateway)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(gateway)).To(Succeed())
			})
			It("Should create a deployment with proper BASE_URL env var", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(LoadResource(stack.Name, "auth", deployment)).To(Succeed())
					return deployment.Spec.Template.Spec.Containers[0].Env
				}).Should(ContainElements(
					core.Env("BASE_URL", "http://gateway:8080/api/auth"),
				))
			})
			Context("with an ingress", func() {
				BeforeEach(func() {
					gateway.Spec.Ingress = &v1beta1.GatewayIngress{
						Host:   "example.net",
						Scheme: "https",
					}
				})
				It("Should create a deployment with proper BASE_URL env var", func() {
					deployment := &appsv1.Deployment{}
					Eventually(func(g Gomega) []corev1.EnvVar {
						g.Expect(LoadResource(stack.Name, "auth", deployment)).To(Succeed())
						return deployment.Spec.Template.Spec.Containers[0].Env
					}).Should(ContainElements(
						core.Env("BASE_URL", "https://example.net/api/auth"),
					))
				})
			})
		})
	})
})
