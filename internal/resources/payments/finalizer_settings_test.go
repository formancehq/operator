package payments

import (
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	settingspkg "github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestCleanSkipsTemporalCleanupWhenSettingIsFalse(t *testing.T) {
	t.Parallel()

	stack := &v1beta1.Stack{
		ObjectMeta: testutil.ObjectMeta("stack0"),
		Spec:       v1beta1.StackSpec{Version: "v3.0.0"},
	}
	payments := &v1beta1.Payments{
		TypeMeta: metav1.TypeMeta{APIVersion: "formance.com/v1beta1", Kind: "Payments"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "payments0",
			UID:  types.UID("payments-uid"),
		},
		Spec: v1beta1.PaymentsSpec{
			StackDependency: v1beta1.StackDependency{Stack: "stack0"},
		},
	}
	ctx := testutil.NewContext(
		stack,
		payments,
		settingspkg.New("clear-temporal", "payments.clear-temporal", "false", "stack0"),
	)

	require.NoError(t, Clean(ctx, payments))

	job := &batchv1.Job{}
	err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: "payments-uid-clean-payments-temporal", Namespace: "stack0"}, job)
	require.Error(t, err)
}
