package tests_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gtypes "github.com/onsi/gomega/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

// STRIX-33: additional coverage for the crypto key lifecycle beyond the
// regression: external override, malformed values (explicit failure, never
// silent replacement), missing external secret, recovery when the external
// secret appears or is fixed later, explicit rotation restarts the deployment
// (generated and external), mode switches, and the signing key secret stays
// untouched.
func randomCryptoKeyValue() []byte {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	Expect(err).NotTo(HaveOccurred())
	return []byte(base64.StdEncoding.EncodeToString(key))
}

// seedCryptoKeySecret creates (or breaks) the generated auth-crypto-key secret
// once the stack namespace exists, racing the operator on purpose: whatever
// wins, the secret ends up with the wanted (possibly malformed) content and
// the operator must never replace it.
func seedCryptoKeySecret(stackName string, mutate func(*corev1.Secret)) {
	Eventually(func(g Gomega) error {
		existing := &corev1.Secret{}
		err := Get(types.NamespacedName{Namespace: stackName, Name: "auth-crypto-key"}, existing)
		switch {
		case k8serrors.IsNotFound(err):
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth-crypto-key",
					Namespace: stackName,
					Labels: map[string]string{
						v1beta1.StackLabel: stackName,
					},
				},
				Type: corev1.SecretTypeOpaque,
			}
			mutate(secret)
			return Create(secret)
		case err != nil:
			return err
		}
		mutate(existing)
		return Update(existing)
	}, "10s", "200ms").Should(Succeed())
}

// expectAuthReconciliationFailed waits until the Auth reports a reconcile
// failure that is the crypto error itself, not an earlier generic failure
// (e.g. "database is not ready"). Each part must appear in Status.Info, which
// carries err.Error() of the failed reconciliation.
func expectAuthReconciliationFailed(auth *v1beta1.Auth, messageParts ...string) {
	authReloaded := &v1beta1.Auth{}
	matchers := make([]gtypes.GomegaMatcher, 0, len(messageParts)+1)
	matchers = append(matchers, ContainSubstring("creating deployment"))
	for _, part := range messageParts {
		matchers = append(matchers, ContainSubstring(part))
	}
	Eventually(func(g Gomega) string {
		g.Expect(LoadResource("", auth.Name, authReloaded)).To(Succeed())
		return authReloaded.Status.Info
	}).Should(And(matchers...))
}

func expectDeploymentCryptoKeyFrom(stackName, secretName, key string) {
	deployment := &appsv1.Deployment{}
	Eventually(func(g Gomega) bool {
		g.Expect(LoadResource(stackName, "auth", deployment)).To(Succeed())
		for _, e := range deployment.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "CRYPTO_KEY" && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil &&
				e.ValueFrom.SecretKeyRef.Name == secretName && e.ValueFrom.SecretKeyRef.Key == key {
				return true
			}
		}
		return false
	}).Should(BeTrue(), "CRYPTO_KEY must come from secret %s/%s", secretName, key)
}

var _ = Describe("AuthController crypto key lifecycle", func() {
	var (
		stack            *v1beta1.Stack
		auth             *v1beta1.Auth
		databaseSettings *v1beta1.Settings
		external         *corev1.Secret
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

	Context("With an external crypto key secret", func() {
		var external *corev1.Secret
		BeforeEach(func() {
			external = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uuid.NewString(),
					Namespace: stack.Name,
					Labels: map[string]string{
						v1beta1.StackLabel: stack.Name,
					},
				},
				Data: map[string][]byte{
					"crypto-key": randomCryptoKeyValue(),
				},
			}
			auth.Spec.CryptoKeyFromSecret = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: external.Name},
				Key:                  "crypto-key",
			}
		})
		JustBeforeEach(func() {
			Eventually(func() error {
				return Create(external)
			}).Should(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(external)).To(Succeed())
		})

		It("injects CRYPTO_KEY from the external secret without taking ownership", func() {
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return LoadResource(stack.Name, "auth", deployment)
			}).Should(Succeed())
			env := deployment.Spec.Template.Spec.Containers[0].Env
			found := false
			for _, e := range env {
				if e.Name == "CRYPTO_KEY" && e.ValueFrom != nil &&
					e.ValueFrom.SecretKeyRef != nil &&
					e.ValueFrom.SecretKeyRef.Name == external.Name &&
					e.ValueFrom.SecretKeyRef.Key == "crypto-key" {
					found = true
				}
			}
			Expect(found).To(BeTrue(), "CRYPTO_KEY must come from the external secret")

			By("not creating or owning an auth-crypto-key secret", func() {
				owned := &corev1.Secret{}
				Expect(LoadResource(stack.Name, "auth-crypto-key", owned)).To(HaveOccurred(),
					"the operator must not generate an auth-crypto-key secret when an external one is referenced")
			})
		})

		It("rolls the deployment when the external secret is rotated", func() {
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return LoadResource(stack.Name, "auth", deployment)
			}).Should(Succeed())
			initial := deployment.Spec.Template.Annotations["auth-crypto-key"]
			Expect(initial).NotTo(BeEmpty())

			newValue := randomCryptoKeyValue()
			posted := &corev1.Secret{}
			Expect(LoadResource(stack.Name, external.Name, posted)).To(Succeed())
			posted.Data["crypto-key"] = newValue
			Expect(Update(posted)).To(Succeed())
			Eventually(func(g Gomega) string {
				d := &appsv1.Deployment{}
				g.Expect(LoadResource(stack.Name, "auth", d)).To(Succeed())
				return d.Spec.Template.Annotations["auth-crypto-key"]
			}).ShouldNot(Equal(initial), "rotating the external secret must restart the auth deployment")
		})
	})

	Context("With a missing external crypto key secret", func() {
		BeforeEach(func() {
			auth.Spec.CryptoKeyFromSecret = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "does-not-exist"},
				Key:                  "crypto-key",
			}
		})
		It("fails the reconciliation without creating a generated secret", func() {
			expectAuthReconciliationFailed(auth, "retrieving external crypto key secret")

			missing := &corev1.Secret{}
			Expect(LoadResource(stack.Name, "auth-crypto-key", missing)).To(HaveOccurred(),
				"no generated secret must silently replace a missing external one")
		})
	})

	Context("With a malformed generated crypto key secret", func() {
		JustBeforeEach(func() {
			// Pre-existing secret with a wrong key length: the operator must
			// fail explicitly instead of silently rotating the key.
			broken := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth-crypto-key",
					Namespace: stack.Name,
					Labels: map[string]string{
						v1beta1.StackLabel: stack.Name,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"crypto-key": []byte(base64.StdEncoding.EncodeToString([]byte("too-short"))),
				},
			}
			Eventually(func(g Gomega) error {
				existing := &corev1.Secret{}
				err := Get(types.NamespacedName{Namespace: stack.Name, Name: "auth-crypto-key"}, existing)
				if err != nil {
					if !k8serrors.IsNotFound(err) {
						return err
					}
					return Create(broken)
				}
				// The operator may have won the race and generated a valid key:
				// break it, the reconciliation must then fail explicitly.
				existing.Data["crypto-key"] = broken.Data["crypto-key"]
				g.Expect(Update(existing)).To(Succeed())
				return nil
			}, "5s", "200ms").Should(Succeed())
		})
		It("fails explicitly and never replaces the existing value", func() {
			expectAuthReconciliationFailed(auth, "invalid crypto key in secret")

			secret := &corev1.Secret{}
			Expect(LoadResource(stack.Name, "auth-crypto-key", secret)).To(Succeed())
			Expect(secret.Data["crypto-key"]).
				To(Equal([]byte(base64.StdEncoding.EncodeToString([]byte("too-short")))),
					"a malformed crypto key must never be silently replaced")
		})
	})

	Context("With a malformed external crypto key secret", func() {
		BeforeEach(func() {
			external = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uuid.NewString(),
					Namespace: stack.Name,
					Labels: map[string]string{
						v1beta1.StackLabel: stack.Name,
					},
				},
				Data: map[string][]byte{
					"crypto-key": []byte("not-base64-valid-padding!"),
				},
			}
			auth.Spec.CryptoKeyFromSecret = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: external.Name},
				Key:                  "crypto-key",
			}
		})
		JustBeforeEach(func() {
			Eventually(func() error {
				return Create(external)
			}).Should(Succeed())
		})
		It("fails the reconciliation", func() {
			expectAuthReconciliationFailed(auth, "invalid crypto key in external secret")
		})
	})

	It("keeps the signing key secret untouched by crypto key reconciliation", func() {
		deployment := &appsv1.Deployment{}
		Eventually(func() error {
			return LoadResource(stack.Name, "auth", deployment)
		}).Should(Succeed())

		signingSecret := &corev1.Secret{}
		Expect(LoadResource(stack.Name, "auth-signing-key", signingSecret)).To(Succeed())
		signingValue := append([]byte(nil), signingSecret.Data["signing-key"]...)
		signingRV := signingSecret.ResourceVersion

		cryptoSecret := &corev1.Secret{}
		Expect(LoadResource(stack.Name, "auth-crypto-key", cryptoSecret)).To(Succeed())

		Expect(LoadResource(stack.Name, "auth-signing-key", signingSecret)).To(Succeed())
		Expect(signingSecret.Data["signing-key"]).To(Equal(signingValue),
			"crypto key lifecycle must not rotate the signing key")
		Expect(signingSecret.ResourceVersion).To(Equal(signingRV))
	})

	Context("With an external crypto key secret created after the initial reconcile", func() {
		var secretName string
		BeforeEach(func() {
			secretName = uuid.NewString()
			auth.Spec.CryptoKeyFromSecret = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  "crypto-key",
			}
		})
		It("recovers through the watched reconciliation once the secret exists", func() {
			By("failing the initial reconciliation while the secret is missing", func() {
				expectAuthReconciliationFailed(auth, "retrieving external crypto key secret")
			})
			By("creating the external secret with a valid crypto key", func() {
				Eventually(func() error {
					return Create(&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      secretName,
							Namespace: stack.Name,
							Labels: map[string]string{
								v1beta1.StackLabel: stack.Name,
							},
						},
						Data: map[string][]byte{"crypto-key": randomCryptoKeyValue()},
					})
				}).Should(Succeed())
			})
			expectDeploymentCryptoKeyFrom(stack.Name, secretName, "crypto-key")
			By("not creating a generated secret as fallback", func() {
				owned := &corev1.Secret{}
				Expect(LoadResource(stack.Name, "auth-crypto-key", owned)).To(HaveOccurred(),
					"no generated secret must silently replace a late external one")
			})
		})
	})

	Context("With an invalid external crypto key secret fixed later", func() {
		var secretName string
		var validValue []byte
		BeforeEach(func() {
			secretName = uuid.NewString()
			validValue = randomCryptoKeyValue()
			auth.Spec.CryptoKeyFromSecret = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  "crypto-key",
			}
		})
		JustBeforeEach(func() {
			Eventually(func() error {
				return Create(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: stack.Name,
						Labels: map[string]string{
							v1beta1.StackLabel: stack.Name,
						},
					},
					Data: map[string][]byte{"crypto-key": []byte("not-base64-valid-padding!")},
				})
			}).Should(Succeed())
		})
		It("recovers once the secret content is fixed", func() {
			expectAuthReconciliationFailed(auth, "invalid crypto key in external secret")
			By("fixing the external secret in place", func() {
				posted := &corev1.Secret{}
				Expect(LoadResource(stack.Name, secretName, posted)).To(Succeed())
				posted.Data["crypto-key"] = validValue
				Expect(Update(posted)).To(Succeed())
			})
			expectDeploymentCryptoKeyFrom(stack.Name, secretName, "crypto-key")
		})
	})

	Context("With a valid pre-seeded generated crypto key secret", func() {
		var preseeded []byte
		JustBeforeEach(func() {
			preseeded = randomCryptoKeyValue()
			seedCryptoKeySecret(stack.Name, func(secret *corev1.Secret) {
				secret.Data = map[string][]byte{"crypto-key": preseeded}
			})
		})
		It("keeps the pre-existing valid key instead of generating a new one", func() {
			secret := &corev1.Secret{}
			Eventually(func() error {
				return LoadResource(stack.Name, "auth-crypto-key", secret)
			}).Should(Succeed())
			Expect(secret.Data["crypto-key"]).To(Equal(preseeded),
				"a valid pre-existing crypto key must be preserved")
		})
	})

	Context("With a rotated generated crypto key", func() {
		It("rolls the deployment on the explicit rotation", func() {
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return LoadResource(stack.Name, "auth", deployment)
			}).Should(Succeed())
			initial := deployment.Spec.Template.Annotations["auth-crypto-key-version"]
			Expect(initial).NotTo(BeEmpty())

			By("replacing the generated key explicitly (manual rotation)", func() {
				rotated := &corev1.Secret{}
				Expect(LoadResource(stack.Name, "auth-crypto-key", rotated)).To(Succeed())
				rotated.Data["crypto-key"] = randomCryptoKeyValue()
				Expect(Update(rotated)).To(Succeed())
			})

			Eventually(func(g Gomega) string {
				d := &appsv1.Deployment{}
				g.Expect(LoadResource(stack.Name, "auth", d)).To(Succeed())
				return d.Spec.Template.Annotations["auth-crypto-key-version"]
			}).ShouldNot(Equal(initial), "an explicit rotation of the generated secret must restart the auth deployment")
		})
	})

	Context("With mode switches between generated and external crypto key", func() {
		var external *corev1.Secret
		JustBeforeEach(func() {
			// Start in generated mode and wait for the generated secret.
			secret := &corev1.Secret{}
			Eventually(func() error {
				return LoadResource(stack.Name, "auth-crypto-key", secret)
			}).Should(Succeed())
		})
		AfterEach(func() {
			if external != nil {
				Expect(Delete(external)).To(Succeed())
			}
		})
		It("switches to external and back without deleting the generated key", func() {
			var generatedValue []byte
			By("capturing the generated key", func() {
				generated := &corev1.Secret{}
				Expect(LoadResource(stack.Name, "auth-crypto-key", generated)).To(Succeed())
				generatedValue = append([]byte(nil), generated.Data["crypto-key"]...)
			})

			By("switching to an external secret", func() {
				external = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      uuid.NewString(),
						Namespace: stack.Name,
						Labels: map[string]string{
							v1beta1.StackLabel: stack.Name,
						},
					},
					Data: map[string][]byte{"crypto-key": randomCryptoKeyValue()},
				}
				Expect(Create(external)).To(Succeed())
				patch := client.MergeFrom(auth.DeepCopy())
				auth.Spec.CryptoKeyFromSecret = &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: external.Name},
					Key:                  "crypto-key",
				}
				Expect(Patch(auth, patch)).To(Succeed())
				expectDeploymentCryptoKeyFrom(stack.Name, external.Name, "crypto-key")
			})

			By("switching back to generated mode", func() {
				patch := client.MergeFrom(auth.DeepCopy())
				auth.Spec.CryptoKeyFromSecret = nil
				Expect(Patch(auth, patch)).To(Succeed())
				expectDeploymentCryptoKeyFrom(stack.Name, "auth-crypto-key", "crypto-key")

				generated := &corev1.Secret{}
				Expect(LoadResource(stack.Name, "auth-crypto-key", generated)).To(Succeed())
				Expect(generated.Data["crypto-key"]).To(Equal(generatedValue),
					"the generated key must survive a mode switch round-trip")

				resourceReference := &v1beta1.ResourceReference{}
				Expect(Get(types.NamespacedName{
					Name: fmt.Sprintf("%s-auth-crypto-key", auth.Name),
				}, resourceReference)).To(HaveOccurred(),
					"no crypto ResourceReference must ever be created: the external path is a plain read-only watch")
			})
		})
	})

	DescribeTable("rejecting malformed generated crypto key secrets without replacement",
		func(setup func() (func(*corev1.Secret), []byte, string)) {
			mutate, expected, errorPart := setup()
			seedCryptoKeySecret(stack.Name, mutate)
			expectAuthReconciliationFailed(auth, errorPart)
			By("settling on the seeded value, then holding it", func() {
				Eventually(func(g Gomega) []byte {
					secret := &corev1.Secret{}
					g.Expect(LoadResource(stack.Name, "auth-crypto-key", secret)).To(Succeed())
					return secret.Data["crypto-key"]
				}).Should(Equal(expected), "the seeded value must be visible")
				Consistently(func(g Gomega) []byte {
					secret := &corev1.Secret{}
					g.Expect(LoadResource(stack.Name, "auth-crypto-key", secret)).To(Succeed())
					return secret.Data["crypto-key"]
				}, "2s", "200ms").Should(Equal(expected),
					"a malformed crypto key must never be silently replaced")
			})
		},
		Entry("nil Data map", func() (func(*corev1.Secret), []byte, string) {
			return func(s *corev1.Secret) { s.Data = nil }, nil, "entry is missing or empty"
		}),
		Entry("missing crypto-key entry", func() (func(*corev1.Secret), []byte, string) {
			// The "crypto-key" entry stays absent: the operator must not add it.
			return func(s *corev1.Secret) { s.Data = map[string][]byte{"other": []byte("value")} }, nil, "entry is missing or empty"
		}),
		Entry("empty crypto-key value", func() (func(*corev1.Secret), []byte, string) {
			// An empty entry hits the explicit missing-or-empty guard.
			return func(s *corev1.Secret) { s.Data = map[string][]byte{"crypto-key": {}} }, []byte{}, "entry is missing or empty"
		}),
		Entry("malformed base64 string", func() (func(*corev1.Secret), []byte, string) {
			return func(s *corev1.Secret) {
				s.Data = map[string][]byte{"crypto-key": []byte("!!!not-base64!!!")}
			}, []byte("!!!not-base64!!!"), "invalid crypto key in secret"
		}),
		Entry("wrong decoded length", func() (func(*corev1.Secret), []byte, string) {
			value := []byte(base64.StdEncoding.EncodeToString([]byte("too-short")))
			return func(s *corev1.Secret) { s.Data = map[string][]byte{"crypto-key": value} }, value, "invalid crypto key in secret"
		}),
		Entry("unpadded value", func() (func(*corev1.Secret), []byte, string) {
			value := []byte(strings.TrimRight(string(randomCryptoKeyValue()), "="))
			return func(s *corev1.Secret) { s.Data = map[string][]byte{"crypto-key": value} }, value, "invalid crypto key in secret"
		}),
		Entry("URL-safe alphabet", func() (func(*corev1.Secret), []byte, string) {
			// Deterministic bytes: their standard base64 contains '+' and '/',
			// so the URL-safe encoding contains '-' and '_' at those positions
			// and can never be mistaken for a canonical standard encoding.
			key := bytes.Repeat([]byte{0xFB}, 32)
			standard := base64.StdEncoding.EncodeToString(key)
			Expect(standard).To(SatisfyAll(ContainSubstring("+"), ContainSubstring("/")),
				"fixture bytes must exercise the non-standard alphabet")
			value := []byte(base64.URLEncoding.EncodeToString(key))
			Expect(string(value)).To(SatisfyAll(ContainSubstring("-"), ContainSubstring("_")),
				"URL-safe fixture must contain URL-safe characters")
			Expect(string(value)).NotTo(Equal(standard))
			return func(s *corev1.Secret) { s.Data = map[string][]byte{"crypto-key": value} }, value, "invalid crypto key in secret"
		}),
		Entry("embedded CR/LF (non-canonical)", func() (func(*corev1.Secret), []byte, string) {
			value := []byte("\r\n" + string(randomCryptoKeyValue()) + "\n")
			return func(s *corev1.Secret) { s.Data = map[string][]byte{"crypto-key": value} }, value, "invalid crypto key in secret"
		}),
		Entry("non-canonical trailing bits", func() (func(*corev1.Secret), []byte, string) {
			value := mutateTrailingBits()
			return func(s *corev1.Secret) { s.Data = map[string][]byte{"crypto-key": value} }, value, "invalid crypto key in secret"
		}),
	)
})

// mutateTrailingBits returns a 44-char standard base64 value of exactly 32
// bytes (one '=' padding character, the last data character at index 42,
// len-2) whose last data character was changed only in its two unused padding
// bits: the decoded bytes are identical, so the value still encodes the same
// key, but its canonical re-encoding differs. Mutating index 41 (len-3)
// instead would alter actual key bits and could yield a different key.
func mutateTrailingBits() []byte {
	valid := []byte(randomCryptoKeyValue())
	Expect(valid).To(HaveLen(44))
	Expect(string(valid)).To(SatisfyAll(HaveLen(44), HaveSuffix("=")),
		"32 bytes must encode to 44 standard base64 chars with exactly one '=' padding character")
	Expect(valid[43]).To(Equal(byte('=')))
	Expect(valid[42]).NotTo(Equal(byte('=')), "index 42 must be the last data character")

	mutated := append([]byte(nil), valid...)
	position := 42 // len-2: last data character, carries two unused bits
	idx := strings.IndexByte(base64Alphabet, mutated[position])
	Expect(idx).To(BeNumerically(">=", 0), "fixture must stay within the standard alphabet")
	// Setting the lowest of the two unused bits keeps the decoded bytes
	// identical while making the padding bits non-canonical.
	mutated[position] = base64Alphabet[idx|0b01]
	Expect(mutated).NotTo(Equal(valid), "the mutation must actually change the value")

	decodedValid, err := base64.StdEncoding.DecodeString(string(valid))
	Expect(err).NotTo(HaveOccurred())
	decodedMutated, err := base64.StdEncoding.DecodeString(string(mutated))
	Expect(err).NotTo(HaveOccurred(), "the mutated value must still decode")
	Expect(decodedMutated).To(HaveLen(32))
	Expect(decodedMutated).To(Equal(decodedValid),
		"mutating only the unused padding bits must preserve the decoded key bytes")
	return mutated
}

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
