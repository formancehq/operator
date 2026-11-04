package tests_test

// STRIX-33 S1 focused regression: the cryptoKeyFromSecret contract is a local
// read-only override of a user-managed Secret. The operator must never require
// the formance.com/stack label, never take ownership of the external Secret
// (no ownerReference, no data/metadata rewrite) and never delete it.
//
// Synthetic envtest only: no live cluster, probe or exploit. This file is new
// and must not touch the immutable regressions
// (auth_crypto_key_test.go, auth_crypto_key_preseed_rotation_test.go).

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

// newExternalCryptoKeyAuth wires a Stack + Settings + Auth with a
// cryptoKeyFromSecret selector pointing at an external Secret.
func newExternalCryptoKeyAuth(secretName string) (*v1beta1.Stack, *v1beta1.Auth) {
	stack := &v1beta1.Stack{
		ObjectMeta: RandObjectMeta(),
		Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
	}
	auth := &v1beta1.Auth{
		ObjectMeta: RandObjectMeta(),
		Spec: v1beta1.AuthSpec{
			StackDependency: v1beta1.StackDependency{
				Stack: stack.Name,
			},
			CryptoKeyFromSecret: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  "crypto-key",
			},
		},
	}
	return stack, auth
}

// expectAuthDeploymentCryptoKeyFrom waits until the auth Deployment exists and
// selects CRYPTO_KEY from the given external secret (asynchronous: no eager
// immediate expectation).
func expectAuthDeploymentCryptoKeyFrom(stackName, secretName string) {
	deployment := &appsv1.Deployment{}
	Eventually(func(g Gomega) bool {
		err := LoadResource(stackName, "auth", deployment)
		g.Expect(err).NotTo(HaveOccurred(), "auth Deployment must be created")
		for _, e := range deployment.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "CRYPTO_KEY" && e.ValueFrom != nil &&
				e.ValueFrom.SecretKeyRef != nil &&
				e.ValueFrom.SecretKeyRef.Name == secretName &&
				e.ValueFrom.SecretKeyRef.Key == "crypto-key" {
				return true
			}
		}
		return false
	}, "30s", "500ms").Should(BeTrue(),
		"CRYPTO_KEY must eventually come from the external secret %s", secretName)
}

// waitStackNamespaceExists is a fixture-only synchronization step: the
// per-stack Namespace is created asynchronously by the Stack reconciler
// (WithOwn[*Stack](&corev1.Namespace{})), so namespaced fixture creates right
// after Create(stack) may race it. Wait once for the Namespace to exist
// before any namespaced create. No crypto behavior is asserted here and no
// timeout policy is changed.
func waitStackNamespaceExists(stackName string) {
	Eventually(func() error {
		return Get(core.GetResourceName(stackName), &corev1.Namespace{})
	}, "10s", "200ms").Should(Succeed())
}

var _ = Describe("AuthController external crypto key secret read-only contract (STRIX-33 S1)", func() {
	Context("With a valid external secret WITHOUT the stack label", func() {
		var (
			stack    *v1beta1.Stack
			auth     *v1beta1.Auth
			external *corev1.Secret
		)

		BeforeEach(func() {
			stack, auth = newExternalCryptoKeyAuth(uuid.NewString())
			// No Labels at all: a standard local SecretKeySelector must be
			// honored without the formance.com/stack replication label.
			external = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      auth.Spec.CryptoKeyFromSecret.Name,
					Namespace: stack.Name,
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"crypto-key": randomCryptoKeyValue(),
				},
			}
		})

		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			waitStackNamespaceExists(stack.Name)
			Expect(Create(settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name))).To(Succeed())
			Expect(Create(external)).To(Succeed())
			Expect(Create(auth)).To(Succeed())
		})

		AfterEach(func() {
			Expect(Delete(external)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})

		It("selects it and configures CRYPTO_KEY from it", func() {
			expectAuthDeploymentCryptoKeyFrom(stack.Name, external.Name)
		})
	})

	Context("With a valid labeled external secret", func() {
		var (
			stack    *v1beta1.Stack
			auth     *v1beta1.Auth
			external *corev1.Secret
			// snapshot of the external secret right after creation
			snapshot *corev1.Secret
		)

		BeforeEach(func() {
			stack, auth = newExternalCryptoKeyAuth(uuid.NewString())
			external = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      auth.Spec.CryptoKeyFromSecret.Name,
					Namespace: stack.Name,
					Labels: map[string]string{
						v1beta1.StackLabel: stack.Name,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"crypto-key": randomCryptoKeyValue(),
				},
			}
		})

		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			waitStackNamespaceExists(stack.Name)
			Expect(Create(settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name))).To(Succeed())
			Expect(Create(external)).To(Succeed())
			Expect(Create(auth)).To(Succeed())
		})

		AfterEach(func() {
			Expect(Delete(external)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})

		snapshotExternal := func() {
			snapshot = &corev1.Secret{}
			Expect(LoadResource(stack.Name, external.Name, snapshot)).To(Succeed())
		}

		It("accepts it and never mutates its ownership, data or metadata", func() {
			// Wait for the positive flow first: the Deployment must be
			// configured from the external secret. Asserting the invariants
			// only after this avoids a false early green where the
			// reconciliation never had a chance to mutate anything.
			expectAuthDeploymentCryptoKeyFrom(stack.Name, external.Name)

			By("snapshotting the external secret after the deployment is configured", snapshotExternal)

			By("keeping the crypto key data untouched", func() {
				Expect(string(snapshot.Data["crypto-key"])).
					To(Equal(string(external.Data["crypto-key"])),
						"the operator must never rewrite the external secret data")
			})

			By("adding no ownerReference to the external secret", func() {
				// envtest has no real garbage collector: the concrete failed
				// invariant is the mutation itself, observed on the object.
				for _, ref := range snapshot.OwnerReferences {
					Expect(ref.Kind).NotTo(Equal("ResourceReference"),
						"the operator must never adopt the external secret: found ownerReference %s/%s", ref.Kind, ref.Name)
				}
				Expect(snapshot.OwnerReferences).To(BeEmpty(),
					"the external secret must carry no ownerReference")
			})

			By("keeping the generated auth-crypto-key secret absent", func() {
				generated := &corev1.Secret{}
				Expect(LoadResource(stack.Name, "auth-crypto-key", generated)).To(HaveOccurred(),
					"an external selector must not trigger generation of auth-crypto-key")
			})
		})

		It("keeps the previous external secret alive when the selector switches to another secret", func() {
			expectAuthDeploymentCryptoKeyFrom(stack.Name, external.Name)
			snapshotExternal()

			// Switch the selector to a second valid external secret.
			second := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uuid.NewString(),
					Namespace: stack.Name,
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"crypto-key": randomCryptoKeyValue(),
				},
			}
			Expect(Create(second)).To(Succeed())
			defer func() { Expect(Delete(second)).To(Succeed()) }()

			Expect(LoadResource("", auth.Name, auth)).To(Succeed())
			auth.Spec.CryptoKeyFromSecret.Name = second.Name
			Expect(Update(auth)).To(Succeed())

			// The new selector must be honored...
			expectAuthDeploymentCryptoKeyFrom(stack.Name, second.Name)

			// ...and the old external secret must still exist: switching
			// names is a configuration change, not a license to delete
			// user-managed state.
			old := &corev1.Secret{}
			Eventually(func(g Gomega) {
				g.Expect(LoadResource(stack.Name, external.Name, old)).To(Succeed(),
					"the previous external secret must survive the selector switch")
			}, "10s", "500ms").Should(Succeed())
			Expect(string(old.Data["crypto-key"])).
				To(Equal(string(external.Data["crypto-key"])),
					"the previous external secret must keep its data")
		})
	})
})
