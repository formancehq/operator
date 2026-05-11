package registries

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	"github.com/formancehq/operator/v3/internal/testutil"
)

func TestImageConfigurationNames(t *testing.T) {
	t.Parallel()

	cfg := &ImageConfiguration{Registry: "registry.example.com", Image: "formance/payments", Version: "v1"}
	require.Equal(t, "registry.example.com/formance/payments:v1", cfg.GetFullImageName())
	require.Equal(t, "registry.example.com/formance/payments:v1", cfg.String())

	cfg.Registry = ""
	require.Equal(t, "formance/payments:v1", cfg.GetFullImageName())
}

func TestGetImageConfigurationAppliesSettings(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext(
		settings.New("rewrite", `registries."ghcr.io".images.formancehq/payments.rewrite`, "mirror/payments", "stack0"),
		settings.New("endpoint", `registries."ghcr.io".endpoint`, "mirror.example.com?pullSecret=registry-creds", "stack0"),
	)

	cfg, err := GetImageConfiguration(ctx, "stack0", "ghcr.io/formancehq/payments:v3.0.0")
	require.NoError(t, err)
	require.Equal(t, "mirror.example.com", cfg.Registry)
	require.Equal(t, "mirror/payments", cfg.Image)
	require.Equal(t, "v3.0.0", cfg.Version)
	require.Len(t, cfg.PullSecrets, 1)
	require.Equal(t, "registry-creds", cfg.PullSecrets[0].Name)
}

func TestGetImageConfigurationDefaultsAndErrors(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext()

	cfg, err := GetImageConfiguration(ctx, "stack0", "payments:latest")
	require.NoError(t, err)
	require.Equal(t, "docker.io", cfg.Registry)
	require.Equal(t, "payments", cfg.Image)

	_, err = GetImageConfiguration(ctx, "stack0", "invalid")
	require.Error(t, err)

	ctx = testutil.NewContext(settings.New("endpoint", `registries."docker.io".endpoint`, "mirror.example.com?bad=value", "stack0"))
	_, err = GetImageConfiguration(ctx, "stack0", "payments:latest")
	require.Error(t, err)
}

func TestGetFormanceImages(t *testing.T) {
	t.Parallel()

	ctx := testutil.NewContext()
	stack := &v1beta1.Stack{ObjectMeta: testutil.ObjectMeta("stack0")}

	cfg, err := GetFormanceImage(ctx, stack, "ledger", "")
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/formancehq/ledger:latest", cfg.GetFullImageName())

	cfg, err = GetBenthosImage(ctx, stack, "v1")
	require.NoError(t, err)
	require.Equal(t, "public.ecr.aws/formance-internal/jeffail/benthos:v1", cfg.GetFullImageName())

	cfg, err = GetNatsBoxImage(ctx, stack, "v2")
	require.NoError(t, err)
	require.Equal(t, "docker.io/natsio/nats-box:v2", cfg.GetFullImageName())
}
