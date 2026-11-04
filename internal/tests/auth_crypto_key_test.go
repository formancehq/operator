package tests_test

import (
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

// STRIX-33 reproduction: the Auth reconciliation must provision a persistent
// per-stack symmetric crypto secret (CRYPTO_KEY) for the auth module.
//
// Contract under test:
//   - Deployment env exposes CRYPTO_KEY from Secret "auth-crypto-key", key "crypto-key".
//   - Secret "auth-crypto-key" is owned by the Auth object; its "crypto-key" entry
//     holds the base64 (base64.StdEncoding) STRING of exactly 32 random bytes.
//   - The crypto key is distinct from the RSA SIGNING_KEY.
//   - The value is generated once and preserved across reconciliations.
//   - Different stacks get different keys.
var _ = Describe("AuthController crypto key", func() {
	Context("When creating an Auth object", func() {
		var (
			stack            *v1beta1.Stack
			auth             *v1beta1.Auth
			databaseSettings *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
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
			Expect(Create(auth)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(databaseSettings)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})

		It("provisions a persistent per-stack CRYPTO_KEY secret and injects it in the auth deployment", func() {
			By("exposing CRYPTO_KEY from the auth-crypto-key secret in the deployment env", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "auth", deployment)
				}).Should(Succeed())
				Expect(deployment).To(BeControlledBy(auth))
				Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
					core.EnvFromSecret("CRYPTO_KEY", "auth-crypto-key", "crypto-key"),
				))
			})

			By("creating the per-stack auth-crypto-key secret owned by the Auth object", func() {
				secret := &corev1.Secret{}
				Eventually(func() error {
					return LoadResource(stack.Name, "auth-crypto-key", secret)
				}).Should(Succeed())
				Expect(secret).To(BeControlledBy(auth))

				By("holding the standard base64 encoding of exactly 32 random bytes", func() {
					value := secret.Data["crypto-key"]
					Expect(value).NotTo(BeEmpty())
					decoded, err := base64.StdEncoding.DecodeString(string(value))
					Expect(err).NotTo(HaveOccurred(),
						"crypto-key must be a standard padded base64 string")
					Expect(decoded).To(HaveLen(32),
						"crypto-key must decode to exactly 32 bytes")
				})

				By("keeping the crypto key distinct from the signing key", func() {
					signingKeySecret := &corev1.Secret{}
					Expect(LoadResource(stack.Name, "auth-signing-key", signingKeySecret)).To(Succeed())
					cryptoSecret := &corev1.Secret{}
					Expect(LoadResource(stack.Name, "auth-crypto-key", cryptoSecret)).To(Succeed())
					Expect(fmt.Sprintf("%s", cryptoSecret.Data["crypto-key"])).
						NotTo(Equal(fmt.Sprintf("%s", signingKeySecret.Data["signing-key"])))
				})
			})

			By("preserving the generated key across reconciliations", func() {
				secret := &corev1.Secret{}
				Eventually(func() error {
					return LoadResource(stack.Name, "auth-crypto-key", secret)
				}).Should(Succeed())
				originalKey := append([]byte(nil), secret.Data["crypto-key"]...)

				patch := client.MergeFrom(auth.DeepCopy())
				auth.Spec.Dev = true
				Expect(Patch(auth, patch)).To(Succeed())

				Eventually(func(g Gomega) []corev1.EnvVar {
					deployment := &appsv1.Deployment{}
					g.Expect(LoadResource(stack.Name, "auth", deployment)).To(Succeed())
					return deployment.Spec.Template.Spec.Containers[0].Env
				}).Should(ContainElement(core.Env("CAOS_OIDC_DEV", "1")))

				Expect(LoadResource(stack.Name, "auth-crypto-key", secret)).To(Succeed())
				Expect(secret.Data["crypto-key"]).To(Equal(originalKey),
					"crypto key must be generated once and preserved, never rotated implicitly")
			})
		})

		It("gives different stacks different crypto keys", func() {
			first := &corev1.Secret{}
			Eventually(func() error {
				return LoadResource(stack.Name, "auth-crypto-key", first)
			}).Should(Succeed())

			otherStack := &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
			otherSettings := settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", otherStack.Name)
			otherAuth := &v1beta1.Auth{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.AuthSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: otherStack.Name,
					},
				},
			}
			Expect(Create(otherStack)).To(Succeed())
			Expect(Create(otherSettings)).To(Succeed())
			Expect(Create(otherAuth)).To(Succeed())
			second := &corev1.Secret{}
			Eventually(func() error {
				return LoadResource(otherStack.Name, "auth-crypto-key", second)
			}).Should(Succeed())
			Expect(Delete(otherSettings)).To(Succeed())
			Expect(Delete(otherStack)).To(Succeed())

			Expect(string(second.Data["crypto-key"])).NotTo(Equal(string(first.Data["crypto-key"])),
				"each stack must have its own unique crypto key")
		})
	})
})
