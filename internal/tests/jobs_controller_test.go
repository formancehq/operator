package tests_test

import (
	"fmt"
	"math/rand/v2"
	"strings"

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
	"sigs.k8s.io/controller-runtime/pkg/client"
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

		module = &v1beta1.Ledger{
			TypeMeta: metav1.TypeMeta{
				Kind: "Ledger",
			},
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
				Name:      uuid.NewString()[:8],
				Namespace: stack.Name,
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

		settings = append(
			make([]v1beta1.Settings, 0),
			v1beta1.Settings{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.SettingsSpec{
					Stacks: []string{stack.Name},
					Key:    `jobs.*.spec.template.annotations`,
					Value:  "first=second",
				},
			},
			v1beta1.Settings{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.SettingsSpec{
					Stacks: []string{stack.Name},
					Key:    fmt.Sprintf(`jobs.%s.containers.%s.run-as`, strings.ToLower(module.Kind), job.Spec.Template.Spec.Containers[0].Name),
					Value:  runAS.String(),
				},
			}, v1beta1.Settings{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.SettingsSpec{
					Stacks: []string{stack.Name},
					Key:    fmt.Sprintf(`jobs.%s.init-containers.%s.run-as`, strings.ToLower(module.Kind), job.Spec.Template.Spec.InitContainers[0].Name),
					Value:  runAS.String(),
				},
			},
		)

	})
	JustBeforeEach(func() {
		Expect(Create(stack)).To(Succeed())
		for _, setting := range settings {
			Expect(Create(&setting)).To(Succeed())
		}
		Expect(Create(module)).To(Succeed())

		Eventually(func() error {
			ns := &v1.Namespace{}
			return LoadResource("", stack.Name, ns)
		}).Should(Succeed())

		Eventually(func() error {
			return LoadResource("", module.Name, module)
		}).Should(Succeed())
		module.Kind = "Ledger"
		err := jobs.Handle(TestContext(), module, job.Name, job.Spec.Template.Spec.Containers[0], jobs.Mutator(func(t *batchv1.Job) error {
			t.Spec.Template.Spec.InitContainers = append(make([]v1.Container, 0), job.Spec.Template.Spec.InitContainers...)
			return nil
		}))
		Expect(err).To(Equal(core.NewPendingError()))
	})
	It("Should have annotations set", func() {
		j := &batchv1.Job{}
		Expect(LoadResource(stack.Name, fmt.Sprintf("%s-%s", module.GetUID(), job.Name), j)).To(Succeed())

		Expect(j.Spec.Template.Annotations).ToNot(BeNil())
		Expect(j.Spec.Template.Annotations["first"]).To(Equal("second"))
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
		for _, setting := range settings {
			Expect(Delete(&setting)).To(Succeed())
		}
		Expect(Delete(module)).To(Succeed())
		Expect(client.IgnoreNotFound(Delete(job))).To(Succeed())
		Expect(Delete(stack)).To(Succeed())
	})
})
