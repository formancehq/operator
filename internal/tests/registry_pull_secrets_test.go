package tests

import (
	"slices"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("Connectivity plugin pull secret", func() {
	It("copies to the fixed plugin destination and preserves its reference across source changes", func() {
		stack := &v1beta1.Stack{ObjectMeta: RandObjectMeta(), Spec: v1beta1.StackSpec{Version: "v2.99.0"}}
		source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: uuid.NewString(), Namespace: "default", Labels: map[string]string{v1beta1.StackLabel: stack.Name}}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)}}
		replacement := source.DeepCopy()
		replacement.Name = uuid.NewString()
		replacement.Labels = nil
		replacement.Data = map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"registry.test":{}}}`)}
		setting := settings.New(uuid.NewString(), settings.ConnectivityInstancePullSecrets, source.Name, stack.Name)
		const target = "ghcr-pull-connectivity-plugins"
		ref := &v1beta1.ResourceReference{ObjectMeta: metav1.ObjectMeta{Name: stack.Name + "-" + target}}
		copy := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: target, Namespace: stack.Name}}
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(setting))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(stack))).To(Succeed())
			// Envtest does not run Kubernetes garbage collection.
			Expect(client.IgnoreNotFound(Delete(ref))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(copy))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(source))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(replacement))).To(Succeed())
		})
		Expect(Create(stack, source)).To(Succeed())
		Eventually(func() error { return LoadResource("", stack.Name, &corev1.Namespace{}) }).Should(Succeed())
		Expect(Create(setting)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(LoadResource("", ref.Name, ref)).To(Succeed())
			g.Expect(ref.Spec.Name).To(Equal(source.Name))
			g.Expect(ref.Spec.TargetName).To(Equal(target))
			g.Expect(ref.Status.SyncedResource).To(Equal(target))
			g.Expect(ref.Status.Ready).To(BeTrue())
			g.Expect(ref.Status.ObservedGeneration).To(Equal(ref.Generation))
			g.Expect(metav1.IsControlledBy(ref, stack)).To(BeTrue())
			g.Expect(LoadResource(stack.Name, target, copy)).To(Succeed())
			g.Expect(copy.Data).To(Equal(source.Data))
			g.Expect(copy).To(BeOwnedBy(ref))
			g.Expect(copy.Labels).NotTo(HaveKey(v1beta1.StackLabel))
		}).Should(Succeed())
		referenceUID := ref.UID
		By("restoring a deleted destination through its reference owner watch")
		Expect(Delete(copy)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(LoadResource(stack.Name, target, copy)).To(Succeed())
			g.Expect(copy.Data).To(Equal(source.Data))
		}).Should(Succeed())
		By("retaining previous data until the new source is authorized")
		Expect(Create(replacement)).To(Succeed())
		patch := client.MergeFrom(setting.DeepCopy())
		setting.Spec.Value = replacement.Name
		Expect(Patch(setting, patch)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(LoadResource("", ref.Name, ref)).To(Succeed())
			g.Expect(ref.UID).To(Equal(referenceUID))
			g.Expect(ref.Spec.Name).To(Equal(replacement.Name))
			g.Expect(ref.Status.Ready && ref.Status.ObservedGeneration == ref.Generation).To(BeFalse())
			g.Expect(LoadResource(stack.Name, target, copy)).To(Succeed())
			g.Expect(copy.Data).To(Equal(source.Data))
		}).Should(Succeed())
		patch = client.MergeFrom(replacement.DeepCopy())
		replacement.Labels = map[string]string{v1beta1.StackLabel: stack.Name}
		Expect(Patch(replacement, patch)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(LoadResource("", ref.Name, ref)).To(Succeed())
			g.Expect(ref.UID).To(Equal(referenceUID))
			g.Expect(ref.Status.Ready).To(BeTrue())
			g.Expect(ref.Status.ObservedGeneration).To(Equal(ref.Generation))
			g.Expect(ref.Status.SyncedResource).To(Equal(target))
			g.Expect(LoadResource(stack.Name, target, copy)).To(Succeed())
			g.Expect(copy.Data).To(Equal(replacement.Data))
		}).Should(Succeed())
		By("following rotation of the newly selected source")
		patch = client.MergeFrom(replacement.DeepCopy())
		replacement.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"rotated.test":{}}}`)
		Expect(Patch(replacement, patch)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(LoadResource(stack.Name, target, copy)).To(Succeed())
			g.Expect(copy.Data).To(Equal(replacement.Data))
		}).Should(Succeed())
		patch = client.MergeFrom(setting.DeepCopy())
		setting.Spec.Value = ""
		Expect(Patch(setting, patch)).To(Succeed())
		Eventually(func() error { return LoadResource("", ref.Name, ref) }).Should(BeNotFound())
	})
})

var _ = Describe("Shared Connectivity plugin Policy", func() {
	It("maintains one policy across Settings and Stack deletion, recreation and drift", func() {
		stackA := &v1beta1.Stack{ObjectMeta: RandObjectMeta(), Spec: v1beta1.StackSpec{Version: "v2.99.0"}}
		stackB := &v1beta1.Stack{ObjectMeta: RandObjectMeta(), Spec: v1beta1.StackSpec{Version: "v2.99.0"}}
		setting := settings.New(uuid.NewString(), settings.ConnectivityInstancePullSecrets, "missing-source", stackA.Name, stackB.Name)
		policy := func() *unstructured.Unstructured {
			p := &unstructured.Unstructured{}
			p.SetAPIVersion("connectivity.formance.com/v1alpha1")
			p.SetKind("Policy")
			p.SetName("stack-connectivity-plugin-pull-secrets")
			return p
		}
		assertNamespaces := func(names ...string) {
			slices.Sort(names)
			Eventually(func(g Gomega) {
				p := policy()
				g.Expect(LoadResource("", p.GetName(), p)).To(Succeed())
				g.Expect(p.GetOwnerReferences()).To(BeEmpty())
				spec := p.Object["spec"].(map[string]any)
				g.Expect(spec).NotTo(HaveKey("connectivitySelector"))
				g.Expect(spec["instanceSelector"]).To(BeEmpty())
				values := make([]any, len(names))
				for i, name := range names {
					values[i] = name
				}
				g.Expect(spec["namespaceSelector"]).To(Equal(map[string]any{"matchExpressions": []any{map[string]any{"key": "kubernetes.io/metadata.name", "operator": "In", "values": values}}}))
				g.Expect(spec["mutate"]).To(Equal(map[string]any{"imagePullSecrets": []any{map[string]any{"name": "ghcr-pull-connectivity-plugins"}}}))
				all := &unstructured.UnstructuredList{}
				all.SetAPIVersion(p.GetAPIVersion())
				all.SetKind("PolicyList")
				g.Expect(List(all)).To(Succeed())
				g.Expect(all.Items).To(HaveLen(1))
			}).Should(Succeed())
		}
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(setting))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(stackA))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(stackB))).To(Succeed())
			Eventually(func() error { p := policy(); return LoadResource("", p.GetName(), p) }).Should(BeNotFound())
		})
		// Missing sources keep copies pending but cannot block policy aggregation.
		Expect(Create(stackA, stackB, setting)).To(Succeed())
		assertNamespaces(stackA.Name, stackB.Name)
		p := policy()
		Expect(LoadResource("", p.GetName(), p)).To(Succeed())
		oldUID := p.GetUID()
		Expect(Delete(p)).To(Succeed())
		assertNamespaces(stackA.Name, stackB.Name)
		Expect(LoadResource("", p.GetName(), p)).To(Succeed())
		Expect(p.GetUID()).NotTo(Equal(oldUID))
		patch := client.MergeFrom(p.DeepCopy())
		p.Object["spec"].(map[string]any)["connectivitySelector"] = map[string]any{}
		Expect(Patch(p, patch)).To(Succeed())
		assertNamespaces(stackA.Name, stackB.Name)
		By("handling the last Setting deletion without requiring a primary object")
		Expect(Delete(setting)).To(Succeed())
		Eventually(func() error { return LoadResource("", p.GetName(), policy()) }).Should(BeNotFound())
		setting.ObjectMeta = metav1.ObjectMeta{Name: setting.Name}
		setting.Spec.Stacks = []string{stackA.Name}
		Expect(Create(setting)).To(Succeed())
		assertNamespaces(stackA.Name)
		By("handling the last eligible Stack deletion and its recreation")
		Expect(Delete(stackA)).To(Succeed())
		Eventually(func() error { return LoadResource("", p.GetName(), policy()) }).Should(BeNotFound())
		Eventually(func() error { return LoadResource("", stackA.Name, &v1beta1.Stack{}) }).Should(BeNotFound())
		stackA.ObjectMeta = metav1.ObjectMeta{Name: stackA.Name}
		Expect(Create(stackA)).To(Succeed())
		assertNamespaces(stackA.Name)
	})
})
