package tests_test

import (
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

// STRIX-33 phase3 re-check: focused rotation invariant for an ACCEPTED
// pre-seeded, UNOWNED auth-crypto-key secret (generated/default mode).
//
// Invariant under test (Codex objection): in generated/default mode
// ensureCryptoKeySecret returns an existing valid Secret unchanged and
// unowned, the default deployment deletes the ResourceReference, and the
// controller only watches Secrets it OWNS (WithOwn(&corev1.Secret{}) uses
// EnqueueRequestForOwner). Therefore rotating ONLY the pre-seeded secret's
// Data — with no Auth/spec/settings edit and no manual reconcile — must still
// roll the auth Deployment (template annotation auth-crypto-key-version
// follows the new Secret resourceVersion) because the user explicitly
// requires change tracking/restart on explicit rotation while preserving
// pre-existing valid values.
//
// The pre-existing preseed test forced a reconciliation before checking
// stability; it never tested rotation alone. This file tests rotation alone.
//
// Synthetic envtest only. No repair of the source is attempted here.
const (
	preseedRotationTimeout = 20 * time.Second
	quiescenceWindow       = 3 * time.Second
)

func newValidCryptoKeyData() map[string][]byte {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	Expect(err).NotTo(HaveOccurred())
	return map[string][]byte{
		"crypto-key": []byte(base64.StdEncoding.EncodeToString(key)),
	}
}

// preseedCryptoKeySecretBeforeAuth creates the auth-crypto-key secret as soon
// as the stack namespace exists, BEFORE the Auth is created, with NO owner
// reference. Whatever the operator does elsewhere, the Auth reconciliation
// that starts later must find the secret already present and accept it.
func preseedCryptoKeySecretBeforeAuth(stackName string) []byte {
	data := newValidCryptoKeyData()
	Eventually(func(g Gomega) error {
		existing := &corev1.Secret{}
		err := Get(types.NamespacedName{Namespace: stackName, Name: "auth-crypto-key"}, existing)
		if k8serrors.IsNotFound(err) {
			return Create(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth-crypto-key",
					Namespace: stackName,
					Labels: map[string]string{
						v1beta1.StackLabel: stackName,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: data,
			})
		}
		return err
	}, "10s", "100ms").Should(Succeed())
	return data["crypto-key"]
}

// cryptoKeySecretRV returns the current resourceVersion of the crypto secret.
func cryptoKeySecretRV(stackName string) string {
	secret := &corev1.Secret{}
	Expect(LoadResource(stackName, "auth-crypto-key", secret)).To(Succeed())
	return secret.ResourceVersion
}

// deploymentCryptoKeyVersionAnnotation reads the auth deployment's
// auth-crypto-key-version template annotation and its generation state.
func deploymentCryptoKeyVersion(stackName string) (annotation string, generation int64, observed int64) {
	deployment := &appsv1.Deployment{}
	Expect(LoadResource(stackName, "auth", deployment)).To(Succeed())
	return deployment.Spec.Template.Annotations["auth-crypto-key-version"],
		deployment.Generation,
		deployment.Status.ObservedGeneration
}

var _ = Describe("AuthController crypto key preseed rotation (unowned secret)", func() {
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
	AfterEach(func() {
		Expect(Delete(databaseSettings)).To(Succeed())
		Expect(Delete(stack)).To(Succeed())
	})

	It("rolls the auth deployment when ONLY the unowned pre-seeded crypto secret is rotated", func() {
		By("creating stack and settings first", func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(databaseSettings)).To(Succeed())
		})

		By("pre-seeding a valid auth-crypto-key secret with no owner BEFORE Auth creation", func() {
			preseedCryptoKeySecretBeforeAuth(stack.Name)
		})

		var preseededOwnerRefs []metav1.OwnerReference
		By("checking the pre-seeded secret carries no owner reference", func() {
			secret := &corev1.Secret{}
			Expect(LoadResource(stack.Name, "auth-crypto-key", secret)).To(Succeed())
			preseededOwnerRefs = secret.OwnerReferences
			Expect(preseededOwnerRefs).To(BeEmpty(),
				"the pre-seeded secret must be unowned so the test exercises the unowned route")
		})

		By("creating the Auth and waiting for the initial deployment", func() {
			Expect(Create(auth)).To(Succeed())
			Eventually(func(g Gomega) string {
				annotation, generation, observed := deploymentCryptoKeyVersionOf(stack.Name, g)
				g.Expect(observed).To(Equal(generation))
				return annotation
			}, "60s", "200ms").ShouldNot(BeEmpty(),
				"the auth deployment must embed the crypto key version annotation")
		})

		var initialAnnotation string
		var initialSecretRV string
		By("waiting for quiescence (no other reconciliation masking the rotation)", func() {
			Eventually(func(g Gomega) string {
				annotation, generation, observed := deploymentCryptoKeyVersionOf(stack.Name, g)
				g.Expect(observed).To(Equal(generation))
				return annotation
			}, "60s", "200ms").ShouldNot(BeEmpty())
			_, generation, observed := deploymentCryptoKeyVersion(stack.Name)
			Expect(observed).To(Equal(generation))

			initialAnnotation, _, _ = deploymentCryptoKeyVersion(stack.Name)
			initialSecretRV = cryptoKeySecretRV(stack.Name)
			Expect(initialAnnotation).To(Equal(initialSecretRV),
				"the deployment annotation must track the accepted pre-seed secret resourceVersion")

			// Quiesce: the annotation must hold steady for a few seconds, so
			// any later change comes from the rotation itself, not from
			// in-flight reconciliations of other resources.
			Consistently(func(g Gomega) string {
				annotation, generation, observed := deploymentCryptoKeyVersionOf(stack.Name, g)
				g.Expect(observed).To(Equal(generation))
				return annotation
			}, quiescenceWindow, "200ms").Should(Equal(initialAnnotation))
		})

		newValue := newValidCryptoKeyData()["crypto-key"]
		var rotatedSecretRV string
		By("rotating ONLY the secret Data (no Auth/spec/settings edit, no manual reconcile)", func() {
			secret := &corev1.Secret{}
			Expect(LoadResource(stack.Name, "auth-crypto-key", secret)).To(Succeed())
			secret.Data = map[string][]byte{"crypto-key": newValue}
			Expect(Update(secret)).To(Succeed())
			rotatedSecretRV = cryptoKeySecretRV(stack.Name)
			Expect(rotatedSecretRV).NotTo(Equal(initialSecretRV))
		})

		By("asserting the deployment template annotation follows the new secret resourceVersion (bounded 20s)", func() {
			Eventually(func(g Gomega) string {
				annotation, generation, observed := deploymentCryptoKeyVersionOf(stack.Name, g)
				g.Expect(observed).To(Equal(generation))
				return annotation
			}, preseedRotationTimeout, "200ms").Should(Equal(rotatedSecretRV),
				"the deployment template annotation must follow the rotated secret resourceVersion")
		})

		By("confirming the pre-existing valid value was preserved until the explicit rotation only", func() {
			secret := &corev1.Secret{}
			Expect(LoadResource(stack.Name, "auth-crypto-key", secret)).To(Succeed())
			Expect(secret.Data["crypto-key"]).To(Equal(newValue))
			Expect(secret.OwnerReferences).To(BeEmpty(),
				"the operator must not claim ownership of the pre-seeded secret")
		})
	})

	It("positive control: rolls the deployment when the OPERATOR-OWNED generated crypto secret is rotated", func() {
		By("creating stack, settings and auth without pre-seed (operator generates the secret)", func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(databaseSettings)).To(Succeed())
			Expect(Create(auth)).To(Succeed())
		})

		By("waiting for the generated owned secret and the initial deployment", func() {
			secret := &corev1.Secret{}
			Eventually(func() error {
				return LoadResource(stack.Name, "auth-crypto-key", secret)
			}, "60s", "200ms").Should(Succeed())
			Expect(secret.OwnerReferences).NotTo(BeEmpty(),
				"the generated secret must be owned by the Auth")

			Eventually(func(g Gomega) string {
				annotation, generation, observed := deploymentCryptoKeyVersionOf(stack.Name, g)
				g.Expect(observed).To(Equal(generation))
				return annotation
			}, "60s", "200ms").ShouldNot(BeEmpty())
		})

		initialAnnotation, _, _ := deploymentCryptoKeyVersion(stack.Name)
		Expect(initialAnnotation).NotTo(BeEmpty())

		By("rotating the owned secret Data", func() {
			secret := &corev1.Secret{}
			Expect(LoadResource(stack.Name, "auth-crypto-key", secret)).To(Succeed())
			secret.Data = newValidCryptoKeyData()
			Expect(Update(secret)).To(Succeed())
		})

		By("asserting the deployment rolls (bounded 20s)", func() {
			Eventually(func(g Gomega) string {
				annotation, generation, observed := deploymentCryptoKeyVersionOf(stack.Name, g)
				g.Expect(observed).To(Equal(generation))
				return annotation
			}, preseedRotationTimeout, "200ms").ShouldNot(Equal(initialAnnotation),
				"an owned generated secret rotation must roll the auth deployment")
		})
	})
})

// deploymentCryptoKeyVersionOf is the gomega-injecting variant used inside
// Eventually/Consistently bodies.
func deploymentCryptoKeyVersionOf(stackName string, g Gomega) (string, int64, int64) {
	deployment := &appsv1.Deployment{}
	g.Expect(LoadResource(stackName, "auth", deployment)).To(Succeed())
	return deployment.Spec.Template.Annotations["auth-crypto-key-version"],
		deployment.Generation,
		deployment.Status.ObservedGeneration
}
