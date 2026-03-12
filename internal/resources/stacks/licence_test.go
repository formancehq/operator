package stacks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type mockContext struct {
	context.Context
	platform core.Platform
}

func (m *mockContext) GetClient() client.Client    { return nil }
func (m *mockContext) GetScheme() *runtime.Scheme  { return nil }
func (m *mockContext) GetAPIReader() client.Reader { return nil }
func (m *mockContext) GetPlatform() core.Platform  { return m.platform }

func newMockContext(platform core.Platform) core.Context {
	return &mockContext{
		Context:  context.Background(),
		platform: platform,
	}
}

func newStack(name string) *v1beta1.Stack {
	return &v1beta1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Generation: 1,
		},
	}
}

func TestSetLicenceCondition_NoLicence(t *testing.T) {
	stack := newStack("test")
	ctx := newMockContext(core.Platform{LicenceSecret: ""})
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.Nil(t, cond, "no condition should be set when licence is absent")
}

func TestSetLicenceCondition_ValidLicence(t *testing.T) {
	stack := newStack("test")
	ctx := newMockContext(core.Platform{
		LicenceSecret: "my-secret",
		LicenceState:  core.LicenceStateValid,
	})
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, "Valid", cond.Reason)
}

func TestSetLicenceCondition_ExpiredLicence(t *testing.T) {
	stack := newStack("test")
	ctx := newMockContext(core.Platform{
		LicenceSecret: "my-secret",
		LicenceState:  core.LicenceStateExpired,
	})
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "Expired", cond.Reason)
	require.Contains(t, cond.Message, "expired")
}

func TestSetLicenceCondition_InvalidLicence(t *testing.T) {
	stack := newStack("test")
	ctx := newMockContext(core.Platform{
		LicenceSecret:  "my-secret",
		LicenceState:   core.LicenceStateInvalid,
		LicenceMessage: "bad signature",
	})
	setLicenceCondition(ctx, stack)

	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "Invalid", cond.Reason)
	require.Equal(t, "bad signature", cond.Message)
}

func TestSetLicenceCondition_TransitionFromValidToExpired(t *testing.T) {
	stack := newStack("test")

	// First: valid
	ctx := newMockContext(core.Platform{
		LicenceSecret: "my-secret",
		LicenceState:  core.LicenceStateValid,
	})
	setLicenceCondition(ctx, stack)
	cond := stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)

	// Then: expired
	ctx2 := newMockContext(core.Platform{
		LicenceSecret: "my-secret",
		LicenceState:  core.LicenceStateExpired,
	})
	setLicenceCondition(ctx2, stack)
	cond = stack.Status.Conditions.Get("LicenceValid")
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "Expired", cond.Reason)
}

func TestSetLicenceCondition_RemovedWhenSecretCleared(t *testing.T) {
	stack := newStack("test")

	// First: set with valid licence
	ctx := newMockContext(core.Platform{
		LicenceSecret: "my-secret",
		LicenceState:  core.LicenceStateValid,
	})
	setLicenceCondition(ctx, stack)
	require.NotNil(t, stack.Status.Conditions.Get("LicenceValid"))

	// Then: licence secret removed
	ctx2 := newMockContext(core.Platform{LicenceSecret: ""})
	setLicenceCondition(ctx2, stack)
	require.Nil(t, stack.Status.Conditions.Get("LicenceValid"))
}
