package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	coresettings "github.com/formancehq/operator/internal/resources/settings"
	. "github.com/formancehq/operator/internal/tests/internal"
)

var _ = Context("When creating a Application", func() {
	var (
		stack    *v1beta1.Stack
		ledger   *v1beta1.Ledger
		settings = make([]*v1beta1.Settings, 0)
	)
	BeforeEach(func() {
		stack = &v1beta1.Stack{
			ObjectMeta: RandObjectMeta(),
			Spec:       v1beta1.StackSpec{},
		}
		settings = []*v1beta1.Settings{
			coresettings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name),
			coresettings.New(uuid.NewString(), "deployments.*.spec.template.annotations", "annotations=annotations", stack.Name),
		}
		ledger = &v1beta1.Ledger{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.LedgerSpec{
				StackDependency: v1beta1.StackDependency{
					Stack: stack.Name,
				},
			},
		}
	})
	JustBeforeEach(func() {
		Expect(Create(stack)).To(Succeed())
		for _, setting := range settings {
			Expect(Create(setting)).To(Succeed())
		}
		Expect(Create(ledger)).To(Succeed())
	})
	AfterEach(func() {
		Expect(Delete(ledger)).To(Succeed())
		for _, setting := range settings {
			Expect(Delete(setting)).To(Succeed())
		}
		Expect(Delete(stack)).To(Succeed())
	})

	It("Should create a deployment with annotations", func() {
		Eventually(func(g Gomega) map[string]string {
			deployment := &appsv1.Deployment{}
			g.Expect(LoadResource(stack.Name, "ledger", deployment)).To(Succeed())
			return deployment.Spec.Template.Annotations
		}).Should(HaveKeyWithValue("annotations", "annotations"))
	})

	It("Should inject NODE_IP env var in all containers", func() {
		Eventually(func(g Gomega) {
			deployment := &appsv1.Deployment{}
			g.Expect(LoadResource(stack.Name, "ledger", deployment)).To(Succeed())

			// Check init containers
			for _, container := range deployment.Spec.Template.Spec.InitContainers {
				found := false
				for _, env := range container.Env {
					if env.Name == "NODE_IP" {
						found = true
						g.Expect(env.ValueFrom).NotTo(BeNil(), "NODE_IP should use ValueFrom in init container %s", container.Name)
						g.Expect(env.ValueFrom.FieldRef).NotTo(BeNil(), "NODE_IP should use FieldRef in init container %s", container.Name)
						g.Expect(env.ValueFrom.FieldRef.FieldPath).To(Equal("status.hostIP"), "NODE_IP should reference status.hostIP in init container %s", container.Name)
						break
					}
				}
				g.Expect(found).To(BeTrue(), "NODE_IP env var not found in init container %s", container.Name)
			}

			// Check regular containers
			for _, container := range deployment.Spec.Template.Spec.Containers {
				found := false
				for _, env := range container.Env {
					if env.Name == "NODE_IP" {
						found = true
						g.Expect(env.ValueFrom).NotTo(BeNil(), "NODE_IP should use ValueFrom in container %s", container.Name)
						g.Expect(env.ValueFrom.FieldRef).NotTo(BeNil(), "NODE_IP should use FieldRef in container %s", container.Name)
						g.Expect(env.ValueFrom.FieldRef.FieldPath).To(Equal("status.hostIP"), "NODE_IP should reference status.hostIP in container %s", container.Name)
						break
					}
				}
				g.Expect(found).To(BeTrue(), "NODE_IP env var not found in container %s", container.Name)
			}
		}).Should(Succeed())
	})

})
