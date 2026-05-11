package jobs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestHandleCreatesJobWithOptionsAndSettings(t *testing.T) {
	t.Parallel()

	owner := paymentsOwner()
	ctx := testutil.NewContext(
		settings.New("env", "jobs.payments.containers.migrate.env-vars", "FROM_SETTINGS=true,EXISTING=override", "stack0"),
		settings.New("annotations", "jobs.payments.spec.template.annotations", "checksum=abc", "stack0"),
	)
	preCreateCalled := false
	policy := batchv1.PodFailurePolicy{Rules: []batchv1.PodFailurePolicyRule{{
		Action: batchv1.PodFailurePolicyActionFailJob,
		OnExitCodes: &batchv1.PodFailurePolicyOnExitCodesRequirement{
			Operator: batchv1.PodFailurePolicyOnExitCodesOpIn,
			Values:   []int32{1},
		},
	}}}

	err := Handle(ctx, owner, "migrate", corev1.Container{
		Name:  "migrate",
		Image: "payments:test",
		Env:   []corev1.EnvVar{core.Env("EXISTING", "base")},
	},
		PreCreate(func() error {
			preCreateCalled = true
			return nil
		}),
		WithServiceAccount("jobs-sa"),
		WithEnvVars(core.Env("FROM_OPTION", "true")),
		WithImagePullSecrets([]corev1.LocalObjectReference{{Name: "pull-secret"}}),
		WithPodFailurePolicy(policy),
	)
	require.True(t, core.IsApplicationError(err))
	require.True(t, preCreateCalled)

	job := &batchv1.Job{}
	require.NoError(t, ctx.GetClient().Get(context.Background(), types.NamespacedName{
		Namespace: "stack0",
		Name:      "uid-123-migrate",
	}, job))

	require.Equal(t, "jobs-sa", job.Spec.Template.Spec.ServiceAccountName)
	require.Equal(t, []corev1.LocalObjectReference{{Name: "pull-secret"}}, job.Spec.Template.Spec.ImagePullSecrets)
	require.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
	require.NotNil(t, job.Spec.PodFailurePolicy)
	require.Equal(t, "abc", job.Spec.Template.Annotations["checksum"])
	require.Equal(t, map[string]string{
		"EXISTING":      "override",
		"FROM_OPTION":   "true",
		"FROM_SETTINGS": "true",
	}, testutil.EnvMap(job.Spec.Template.Spec.Containers[0].Env))
	require.Len(t, job.OwnerReferences, 1)
	require.Equal(t, "Payments", job.OwnerReferences[0].Kind)
}

func TestHandleReturnsNilWhenExistingJobIsValid(t *testing.T) {
	t.Parallel()

	owner := paymentsOwner()
	existing := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "stack0",
			Name:      "uid-123-migrate",
		},
		Status: batchv1.JobStatus{Succeeded: 1},
	}
	ctx := testutil.NewContext(existing)

	err := Handle(ctx, owner, "migrate", corev1.Container{Name: "migrate"})
	require.NoError(t, err)
}

func TestHandleAppliesRunAsAndInitContainerSettings(t *testing.T) {
	t.Parallel()

	owner := paymentsOwner()
	ctx := testutil.NewContext(
		settings.New("container-run-as", "jobs.payments.containers.migrate.run-as", "user=1000,group=2000", "stack0"),
		settings.New("init-env", "jobs.payments.init-containers.setup.env-vars", "INIT_EXISTING=override,INIT_FROM_SETTINGS=true", "stack0"),
		settings.New("init-run-as", "jobs.payments.init-containers.setup.run-as", "user=1002,group=2002", "stack0"),
	)

	err := Handle(ctx, owner, "migrate", corev1.Container{
		Name:  "migrate",
		Image: "payments:test",
	}, Mutator(func(job *batchv1.Job) error {
		job.Spec.Template.Spec.InitContainers = []corev1.Container{{
			Name:  "setup",
			Image: "setup:test",
			Env:   []corev1.EnvVar{core.Env("INIT_EXISTING", "base")},
		}}
		return nil
	}))
	require.True(t, core.IsApplicationError(err))

	job := &batchv1.Job{}
	require.NoError(t, ctx.GetClient().Get(context.Background(), types.NamespacedName{
		Namespace: "stack0",
		Name:      "uid-123-migrate",
	}, job))

	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	containerSecurityContext := job.Spec.Template.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext)
	require.Equal(t, int64(1000), *containerSecurityContext.RunAsUser)
	require.Equal(t, int64(2000), *containerSecurityContext.RunAsGroup)
	require.True(t, *containerSecurityContext.RunAsNonRoot)

	require.Len(t, job.Spec.Template.Spec.InitContainers, 1)
	initContainer := job.Spec.Template.Spec.InitContainers[0]
	require.Equal(t, map[string]string{
		"INIT_EXISTING":      "override",
		"INIT_FROM_SETTINGS": "true",
	}, testutil.EnvMap(initContainer.Env))
	require.NotNil(t, initContainer.SecurityContext)
	require.Equal(t, int64(1002), *initContainer.SecurityContext.RunAsUser)
	require.Equal(t, int64(2002), *initContainer.SecurityContext.RunAsGroup)
	require.True(t, *initContainer.SecurityContext.RunAsNonRoot)
}

func TestHandleRunsPreCreateErrorBeforeCreatingJob(t *testing.T) {
	t.Parallel()

	owner := paymentsOwner()
	ctx := testutil.NewContext()

	err := Handle(ctx, owner, "migrate", corev1.Container{Name: "migrate"},
		PreCreate(func() error {
			return errPreCreate
		}),
	)
	require.ErrorIs(t, err, errPreCreate)

	job := &batchv1.Job{}
	err = ctx.GetClient().Get(context.Background(), types.NamespacedName{Namespace: "stack0", Name: "uid-123-migrate"}, job)
	require.Error(t, err)
}

var errPreCreate = errSentinel("precreate failed")

type errSentinel string

func (e errSentinel) Error() string {
	return string(e)
}

func paymentsOwner() *v1beta1.Payments {
	return &v1beta1.Payments{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Payments"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "payments",
			UID:  "uid-123",
		},
		Spec: v1beta1.PaymentsSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
}
