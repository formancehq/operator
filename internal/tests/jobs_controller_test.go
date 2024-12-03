package tests_test

import (
	"fmt"
	"math/rand/v2"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/jobs"
	. "github.com/formancehq/operator/internal/tests/internal"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type runAs struct {
	user  int
	group int
}

func (r *runAs) String() string {
	return fmt.Sprintf(`user=%d, group=%d`, r.user, r.group)
}

var _ = Describe("Job", func() {
	var (
		settings []v1beta1.Settings
		stack    *v1beta1.Stack
		module   *v1beta1.Ledger
		job      *batchv1.Job
		runAS    *runAs
	)
	BeforeEach(func() {
		stack = &v1beta1.Stack{
			ObjectMeta: metav1.ObjectMeta{
				Name: uuid.NewString()[:8],
			},
		}
		runAS = &runAs{
			user:  rand.IntN(65534),
			group: rand.IntN(65534),
		}
		settings = append(
			make([]v1beta1.Settings, 0),
			v1beta1.Settings{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.SettingsSpec{
					Stacks: []string{stack.Name},
					Key:    `jobs.*.containers.*.run-as`,
					Value:  runAS.String(),
				},
			}, v1beta1.Settings{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.SettingsSpec{
					Stacks: []string{stack.Name},
					Key:    `jobs.*.init-containers.*.run-as`,
					Value:  runAS.String(),
				},
			},
		)

		module = &v1beta1.Ledger{
			ObjectMeta: metav1.ObjectMeta{
				Name: uuid.NewString()[:8],
			},
			Spec: v1beta1.LedgerSpec{
				StackDependency: v1beta1.StackDependency{
					Stack: stack.Name,
				},
			},
		}
		job = &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name: uuid.NewString()[:8],
			},
			Spec: batchv1.JobSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						InitContainers: []v1.Container{
							{
								Name:  uuid.NewString()[:8],
								Image: "alpine-3.14",
								Command: []string{
									"echo",
									"hello",
								},
							},
						},
						Containers: []v1.Container{
							{
								Name:  uuid.NewString()[:8],
								Image: "alpine-3.14",
								Command: []string{
									"echo",
									"hello",
								},
							},
						},
					},
				},
			},
		}

	})
	JustBeforeEach(func() {
		Expect(Create(stack)).To(Succeed())
		for _, setting := range settings {
			Expect(Create(&setting)).To(Succeed())
		}
		Expect(Create(module)).To(Succeed())

		err := jobs.Handle(TestContext(), module, job.Name, job.Spec.Template.Spec.Containers[0], jobs.Mutator(func(t *batchv1.Job) error {
			t.Spec.Template.Spec.InitContainers = append(make([]v1.Container, 0), job.Spec.Template.Spec.InitContainers...)
			return nil
		}))
		Expect(err).To(Equal(core.NewPendingError()))
	})
	It("Should have security context configured with run-as settings", func() {
		j := &batchv1.Job{}
		Expect(LoadResource(stack.Name, fmt.Sprintf("%s-%s", module.GetUID(), job.Name), j)).To(Succeed())

		for _, container := range j.Spec.Template.Spec.Containers {
			Expect(container.SecurityContext).ToNot(BeNil())
			Expect(container.SecurityContext.RunAsUser).ToNot(BeNil())
			Expect(*container.SecurityContext.RunAsUser).To(Equal(int64(runAS.user)))
			Expect(container.SecurityContext.RunAsGroup).ToNot(BeNil())
			Expect(*container.SecurityContext.RunAsGroup).To(Equal(int64(runAS.group)))
		}

		for _, container := range j.Spec.Template.Spec.InitContainers {
			Expect(container.SecurityContext).ToNot(BeNil())
			Expect(container.SecurityContext.RunAsUser).ToNot(BeNil())
			Expect(*container.SecurityContext.RunAsUser).To(Equal(int64(runAS.user)))
			Expect(container.SecurityContext.RunAsGroup).ToNot(BeNil())
			Expect(*container.SecurityContext.RunAsGroup).To(Equal(int64(runAS.group)))
		}
	})
	AfterEach(func() {
		Expect(Delete(stack)).To(Succeed())
		for _, setting := range settings {
			Expect(Delete(&setting)).To(Succeed())
		}
	})
})
