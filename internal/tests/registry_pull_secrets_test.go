package tests

import (
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("Registry pull secret references", func() {
	It("copies configured credentials, rotates them, and removes obsolete references", func() {
		const registryReferenceLabel = "formance.com/registry-pull-secret"
		stack := &v1beta1.Stack{
			ObjectMeta: RandObjectMeta(),
			Spec:       v1beta1.StackSpec{Version: "v2.99.0"},
		}
		newSource := func() *corev1.Secret {
			return &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uuid.NewString(),
					Namespace: "default",
					Labels:    map[string]string{v1beta1.StackLabel: stack.Name},
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`{"auths":{"mirror.example.test":{"auth":"dGVzdDppbml0aWFs"}}}`),
				},
			}
		}
		source, replacement := newSource(), newSource()
		endpoint := settings.New(uuid.NewString(), `registries."ghcr.io".endpoint`,
			fmt.Sprintf("mirror.example.test?pullSecret=%s", source.Name), stack.Name)
		networkPolicies := settings.New(uuid.NewString(), "networkpolicies.enabled", "true", stack.Name)
		references := func() ([]v1beta1.ResourceReference, error) {
			list := &v1beta1.ResourceReferenceList{}
			if err := List(list, client.MatchingLabels{registryReferenceLabel: "true"}); err != nil {
				return nil, err
			}
			var result []v1beta1.ResourceReference
			for _, reference := range list.Items {
				if reference.Spec.Stack == stack.Name {
					result = append(result, reference)
				}
			}
			return result, nil
		}
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(endpoint))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(networkPolicies))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(stack))).To(Succeed())
			// Envtest has no garbage collector; explicitly remove test-owned objects.
			remaining, err := references()
			Expect(err).NotTo(HaveOccurred())
			for i := range remaining {
				Expect(client.IgnoreNotFound(Delete(&remaining[i]))).To(Succeed())
			}
			for _, secret := range []*corev1.Secret{source, replacement} {
				Expect(client.IgnoreNotFound(Delete(secret))).To(Succeed())
				copy := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: stack.Name, Name: secret.Name}}
				Expect(client.IgnoreNotFound(Delete(copy))).To(Succeed())
			}
		})

		By("creating Settings after the Stack namespace exists, without any module")
		Expect(Create(stack, source)).To(Succeed())
		Eventually(func() error {
			return LoadResource("", stack.Name, &corev1.Namespace{})
		}).Should(Succeed())
		Expect(Create(endpoint)).To(Succeed())

		var initialReference v1beta1.ResourceReference
		Eventually(func(g Gomega) {
			current, err := references()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current).To(HaveLen(1))
			initialReference = current[0]
			g.Expect(initialReference.Spec.Name).To(Equal(source.Name))
			g.Expect(initialReference.Spec.GroupVersionKind).To(Equal(&metav1.GroupVersionKind{Version: "v1", Kind: "Secret"}))
			g.Expect(metav1.IsControlledBy(&initialReference, stack)).To(BeTrue())
			g.Expect(initialReference.Status.Ready).To(BeTrue())
			copy := &corev1.Secret{}
			g.Expect(LoadResource(stack.Name, source.Name, copy)).To(Succeed())
			g.Expect(copy.Type).To(Equal(source.Type))
			g.Expect(copy.Data).To(Equal(source.Data))
			g.Expect(copy).To(BeOwnedBy(&initialReference))
		}).Should(Succeed())

		By("propagating a credential rotation through the source Secret watch")
		patch := client.MergeFrom(source.DeepCopy())
		source.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"mirror.example.test":{"auth":"dGVzdDpyb3RhdGVk"}}}`)
		Expect(Patch(source, patch)).To(Succeed())
		Eventually(func(g Gomega) {
			copy := &corev1.Secret{}
			g.Expect(LoadResource(stack.Name, source.Name, copy)).To(Succeed())
			g.Expect(copy.Data).To(Equal(source.Data))
		}).Should(Succeed())

		By("retaining the old reference while its replacement source is missing")
		patch = client.MergeFrom(endpoint.DeepCopy())
		endpoint.Spec.Value = fmt.Sprintf("mirror.example.test?pullSecret=%s", replacement.Name)
		Expect(Patch(endpoint, patch)).To(Succeed())
		Eventually(func(g Gomega) {
			current, err := references()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current).To(HaveLen(2))
			g.Expect(current).To(ContainElement(And(
				HaveField("Spec.Name", replacement.Name),
				HaveField("Status.Ready", false),
			)))
			g.Expect(LoadResource("", initialReference.Name, &v1beta1.ResourceReference{})).To(Succeed())
		}).Should(Succeed())

		By("creating network policies even while replacement credentials are pending")
		Expect(Create(networkPolicies)).To(Succeed())
		Eventually(func(g Gomega) {
			current, err := references()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current).To(HaveLen(2))
			g.Expect(current).To(ContainElement(And(
				HaveField("Spec.Name", replacement.Name),
				HaveField("Status.Ready", false),
			)))
			policies := &networkingv1.NetworkPolicyList{}
			g.Expect(List(policies, client.InNamespace(stack.Name))).To(Succeed())
			g.Expect(policies.Items).To(ConsistOf(
				HaveField("Name", "allow-same-namespace"),
				HaveField("Name", "allow-gateway-ingress"),
			))
		}).Should(Succeed())

		By("copying the replacement before pruning the obsolete reference")
		Expect(Create(replacement)).To(Succeed())
		Eventually(func(g Gomega) {
			current, err := references()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current).To(HaveLen(1))
			g.Expect(current[0].Spec.Name).To(Equal(replacement.Name))
			g.Expect(current[0].Status.Ready).To(BeTrue())
			copy := &corev1.Secret{}
			g.Expect(LoadResource(stack.Name, replacement.Name, copy)).To(Succeed())
			g.Expect(copy.Data).To(Equal(replacement.Data))
			g.Expect(copy).To(BeOwnedBy(&current[0]))
			g.Expect(LoadResource("", initialReference.Name, &v1beta1.ResourceReference{})).To(BeNotFound())
		}).Should(Succeed())

		By("removing the generated references when Settings is deleted")
		Expect(Delete(endpoint)).To(Succeed())
		Eventually(references).Should(BeEmpty())
		Expect(LoadResource(source.Namespace, source.Name, &corev1.Secret{})).To(Succeed())
		Expect(LoadResource(replacement.Namespace, replacement.Name, &corev1.Secret{})).To(Succeed())
		// Copy deletion is delegated to Kubernetes GC, which envtest does not run.
	})
})

var _ = Describe("Explicit Connectivity pull secrets", func() {
	It("copies the union without registry settings or ready modules and retains shared references", func() {
		stack := &v1beta1.Stack{ObjectMeta: RandObjectMeta(), Spec: v1beta1.StackSpec{Version: "v2.99.0"}}
		source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: uuid.NewString(), Namespace: "default", Labels: map[string]string{v1beta1.StackLabel: stack.Name}}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)}}
		coreSetting := settings.New(uuid.NewString(), settings.ConnectivityPullSecrets, source.Name, stack.Name)
		pluginSetting := settings.New(uuid.NewString(), settings.ConnectivityInstancePullSecrets, source.Name, stack.Name)
		references := func() ([]v1beta1.ResourceReference, error) {
			list := &v1beta1.ResourceReferenceList{}
			if err := List(list, client.MatchingLabels{"formance.com/registry-pull-secret": "true"}); err != nil {
				return nil, err
			}
			var result []v1beta1.ResourceReference
			for _, ref := range list.Items {
				if ref.Spec.Stack == stack.Name {
					result = append(result, ref)
				}
			}
			return result, nil
		}
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(coreSetting))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(pluginSetting))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(stack))).To(Succeed())
			refs, err := references()
			Expect(err).NotTo(HaveOccurred())
			for i := range refs {
				Expect(client.IgnoreNotFound(Delete(&refs[i]))).To(Succeed())
			}
			Expect(client.IgnoreNotFound(Delete(source))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: source.Name, Namespace: stack.Name}}))).To(Succeed())
		})
		Expect(Create(stack, source)).To(Succeed())
		Eventually(func() error { return LoadResource("", stack.Name, &corev1.Namespace{}) }).Should(Succeed())
		Expect(Create(coreSetting, pluginSetting)).To(Succeed())
		Eventually(func(g Gomega) {
			refs, err := references()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(refs).To(HaveLen(1))
			g.Expect(refs[0].Status.Ready).To(BeTrue())
			copy := &corev1.Secret{}
			g.Expect(LoadResource(stack.Name, source.Name, copy)).To(Succeed())
			g.Expect(copy.Data).To(Equal(source.Data))
		}).Should(Succeed())
		policies := func() ([]unstructured.Unstructured, error) {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(schema.GroupVersionKind{Group: "connectivity.formance.com", Version: "v1alpha1", Kind: "PolicyList"})
			if err := List(list); err != nil {
				return nil, err
			}
			var result []unstructured.Unstructured
			for _, policy := range list.Items {
				if metav1.IsControlledBy(&policy, stack) {
					result = append(result, policy)
				}
			}
			return result, nil
		}
		DeferCleanup(func() {
			remaining, err := policies()
			Expect(err).NotTo(HaveOccurred())
			for i := range remaining {
				Expect(client.IgnoreNotFound(Delete(&remaining[i]))).To(Succeed())
			}
		})
		Eventually(func(g Gomega) {
			current, err := policies()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current).To(HaveLen(2))
			axes := map[string]bool{}
			for _, policy := range current {
				spec := policy.Object["spec"].(map[string]any)
				_, core := spec["connectivitySelector"]
				_, plugin := spec["instanceSelector"]
				g.Expect(core != plugin).To(BeTrue())
				if core {
					axes["core"] = true
				} else {
					axes["plugin"] = true
				}
				g.Expect(spec["namespaceSelector"]).To(Equal(map[string]any{"matchLabels": map[string]any{"kubernetes.io/metadata.name": stack.Name}}))
			}
			g.Expect(axes).To(HaveLen(2))
		}).Should(Succeed())
		// Deleting a generated Policy must enqueue its Stack and recreate it.
		current, err := policies()
		Expect(err).NotTo(HaveOccurred())
		Expect(Delete(&current[0])).To(Succeed())
		Eventually(func(g Gomega) {
			remaining, err := policies()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(remaining).To(HaveLen(2))
			for _, p := range remaining {
				g.Expect(p.GetUID()).NotTo(Equal(current[0].GetUID()))
			}
		}).Should(Succeed())
		Expect(Delete(coreSetting)).To(Succeed())
		// A new value forces another Settings event; shared credentials remain needed.
		patch := client.MergeFrom(pluginSetting.DeepCopy())
		pluginSetting.Spec.Value = source.Name + "," + source.Name
		Expect(Patch(pluginSetting, patch)).To(Succeed())
		Eventually(func(g Gomega) {
			refs, err := references()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(refs).To(HaveLen(1))
			g.Expect(refs[0].Status.Ready).To(BeTrue())
		}).Should(Succeed())
		Eventually(func(g Gomega) {
			current, err := policies()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current).To(HaveLen(1))
			g.Expect(current[0].Object["spec"]).To(HaveKey("instanceSelector"))
		}).Should(Succeed())
		Expect(Delete(pluginSetting)).To(Succeed())
		Eventually(policies).Should(BeEmpty())
		Eventually(references).Should(BeEmpty())
	})
})
