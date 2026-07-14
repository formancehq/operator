package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var ledgerV3ClusterGVK = schema.GroupVersionKind{
	Group:   "ledger.formance.com",
	Version: "v1alpha1",
	Kind:    "Cluster",
}

func newLedgerV3Cluster() *unstructured.Unstructured {
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(ledgerV3ClusterGVK)
	return cluster
}

var _ = Describe("Ledger v3 controller", func() {
	var (
		stack  *v1beta1.Stack
		ledger *v1beta1.Ledger
	)

	BeforeEach(func() {
		stack = &v1beta1.Stack{
			ObjectMeta: RandObjectMeta(),
			Spec:       v1beta1.StackSpec{Version: "v3.0.0-alpha.1"},
		}
		ledger = &v1beta1.Ledger{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.LedgerSpec{
				StackDependency: v1beta1.StackDependency{Stack: stack.Name},
			},
		}
	})

	JustBeforeEach(func() {
		Expect(Create(stack)).To(Succeed())
		Expect(Create(ledger)).To(Succeed())
	})

	AfterEach(func() {
		Expect(client.IgnoreNotFound(Delete(ledger))).To(Succeed())
		Expect(client.IgnoreNotFound(Delete(stack))).To(Succeed())
	})

	It("delegates runtime provisioning to a Ledger v3 Cluster", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func(g Gomega) error {
			err := Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cluster).To(BeControlledBy(ledger))
			return nil
		}).Should(Succeed())

		repository, found, err := unstructured.NestedString(cluster.Object, "spec", "image", "repository")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(repository).To(Equal("ghcr.io/formancehq/ledger"))

		tag, found, err := unstructured.NestedString(cluster.Object, "spec", "image", "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(tag).To(Equal("v3.0.0-alpha.1"))

		deployment := &appsv1.Deployment{}
		Consistently(func() bool {
			err := Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())

		database := &v1beta1.Database{}
		Consistently(func() bool {
			err := Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), database)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
	})

	It("reuses and normalizes the historical Ledger replica setting", func() {
		replicaSettings := settings.New(uuid.NewString(), "deployments.ledger.replicas", "4", stack.Name)
		Expect(Create(replicaSettings)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(replicaSettings))).To(Succeed())
		})

		cluster := newLedgerV3Cluster()
		Eventually(func(g Gomega) int64 {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			replicas, found, err := unstructured.NestedInt64(cluster.Object, "spec", "replicas")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return replicas
		}).Should(Equal(int64(5)))
	})

	It("mirrors Cluster readiness on the Formance Ledger", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
		}).Should(Succeed())

		Eventually(func(g Gomega) error {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			cluster.Object["status"] = map[string]any{
				"phase":              "Running",
				"readyReplicas":      int64(3),
				"observedGeneration": cluster.GetGeneration(),
			}
			return TestContext().GetClient().Status().Update(TestContext(), cluster)
		}).Should(Succeed())

		Eventually(func(g Gomega) bool {
			g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			return ledger.Status.Ready
		}).Should(BeTrue())
	})

	It("removes the Cluster when the stack is disabled", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
		}).Should(Succeed())

		Expect(LoadResource("", stack.Name, stack)).To(Succeed())
		patch := client.MergeFrom(stack.DeepCopy())
		stack.Spec.Disabled = true
		Expect(Patch(stack, patch)).To(Succeed())

		Eventually(func() bool {
			err := Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
	})

	It("requires an explicit migration before switching back to v2", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
		}).Should(Succeed())

		Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
		patch := client.MergeFrom(ledger.DeepCopy())
		ledger.Spec.Version = "v2.99.0"
		Expect(Patch(ledger, patch)).To(Succeed())

		Consistently(func() bool {
			err := Get(core.GetNamespacedResourceName(stack.Name, "ledger"), &appsv1.Deployment{})
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())

		Consistently(func() bool {
			err := Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), &v1beta1.Database{})
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())

		Eventually(func(g Gomega) string {
			g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			return ledger.Status.Info
		}).Should(ContainSubstring("migration required"))
	})

	Context("when legacy resources already exist", func() {
		var databaseSettings *v1beta1.Settings

		BeforeEach(func() {
			stack.Spec.Version = "v2.99.0"
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			Expect(Create(databaseSettings)).To(Succeed())
		})

		AfterEach(func() {
			Expect(client.IgnoreNotFound(Delete(databaseSettings))).To(Succeed())
		})

		It("requires an explicit migration before switching to v3", func() {
			Eventually(func() error {
				return Get(core.GetNamespacedResourceName(stack.Name, "ledger"), &appsv1.Deployment{})
			}).Should(Succeed())

			Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			patch := client.MergeFrom(ledger.DeepCopy())
			ledger.Spec.Version = "v3.0.0-alpha.1"
			Expect(Patch(ledger, patch)).To(Succeed())

			cluster := newLedgerV3Cluster()
			Consistently(func() bool {
				err := Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())

			Eventually(func(g Gomega) string {
				g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
				return ledger.Status.Info
			}).Should(ContainSubstring("migration required"))
		})
	})
})
