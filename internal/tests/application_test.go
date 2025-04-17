package tests_test

import (
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	coresettings "github.com/formancehq/operator/internal/resources/settings"
	. "github.com/formancehq/operator/internal/tests/internal"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	appsv1 "k8s.io/api/apps/v1"

	. "github.com/onsi/gomega"
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
		settings = append(settings,
			coresettings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name),
			coresettings.New(uuid.NewString(), "deployments.*.spec.template.annotations", "annotations=annotations", stack.Name),
		)
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

})
