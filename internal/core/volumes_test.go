package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewVolumeFromConfigMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		volumeName string
		configMap *corev1.ConfigMap
	}{
		{
			name:       "simple configmap",
			volumeName: "config",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-config",
				},
			},
		},
		{
			name:       "configmap with data",
			volumeName: "app-config",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "application-config",
				},
				Data: map[string]string{
					"config.yaml": "key: value",
				},
			},
		},
		{
			name:       "configmap with namespace",
			volumeName: "shared-config",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared-config",
					Namespace: "default",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			volume := NewVolumeFromConfigMap(tt.volumeName, tt.configMap)

			require.Equal(t, tt.volumeName, volume.Name)
			require.NotNil(t, volume.VolumeSource.ConfigMap)
			require.Equal(t, tt.configMap.Name, volume.VolumeSource.ConfigMap.Name)
			require.Nil(t, volume.VolumeSource.Secret)
			require.Nil(t, volume.VolumeSource.EmptyDir)
		})
	}
}

func TestNewVolumeFromConfigMapStructure(t *testing.T) {
	t.Parallel()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-config",
		},
	}

	volume := NewVolumeFromConfigMap("test-volume", configMap)

	// Verify the volume source is correctly structured
	require.NotNil(t, volume.VolumeSource.ConfigMap)
	require.Equal(t, "test-config", volume.VolumeSource.ConfigMap.LocalObjectReference.Name)

	// Verify other volume sources are not set
	require.Nil(t, volume.VolumeSource.Secret)
	require.Nil(t, volume.VolumeSource.EmptyDir)
	require.Nil(t, volume.VolumeSource.HostPath)
	require.Nil(t, volume.VolumeSource.PersistentVolumeClaim)
}

func TestNewVolumeMount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		volumeName string
		mountPath string
		readOnly  bool
	}{
		{
			name:       "read-write mount",
			volumeName: "data",
			mountPath:  "/data",
			readOnly:   false,
		},
		{
			name:       "read-only mount",
			volumeName: "config",
			mountPath:  "/etc/config",
			readOnly:   true,
		},
		{
			name:       "nested mount path",
			volumeName: "app-config",
			mountPath:  "/var/lib/app/config",
			readOnly:   true,
		},
		{
			name:       "root mount",
			volumeName: "root",
			mountPath:  "/",
			readOnly:   false,
		},
		{
			name:       "tmp mount",
			volumeName: "tmp",
			mountPath:  "/tmp",
			readOnly:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			volumeMount := NewVolumeMount(tt.volumeName, tt.mountPath, tt.readOnly)

			require.Equal(t, tt.volumeName, volumeMount.Name)
			require.Equal(t, tt.mountPath, volumeMount.MountPath)
			require.Equal(t, tt.readOnly, volumeMount.ReadOnly)
		})
	}
}

func TestNewVolumeMountReadOnlyFlag(t *testing.T) {
	t.Parallel()

	t.Run("readOnly true", func(t *testing.T) {
		t.Parallel()

		mount := NewVolumeMount("config", "/etc/config", true)
		require.True(t, mount.ReadOnly)
	})

	t.Run("readOnly false", func(t *testing.T) {
		t.Parallel()

		mount := NewVolumeMount("data", "/data", false)
		require.False(t, mount.ReadOnly)
	})
}

func TestVolumeAndMountIntegration(t *testing.T) {
	t.Parallel()

	// Test a realistic scenario: creating a volume from a configmap and mounting it

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "app-config",
		},
		Data: map[string]string{
			"config.yaml": "server:\n  port: 8080",
		},
	}

	// Create volume
	volume := NewVolumeFromConfigMap("app-config", configMap)

	// Create mount
	mount := NewVolumeMount("app-config", "/etc/app/config", true)

	// Verify they reference the same volume name
	require.Equal(t, volume.Name, mount.Name,
		"Volume and mount should have matching names")

	// Verify volume points to the configmap
	require.Equal(t, configMap.Name, volume.VolumeSource.ConfigMap.Name)

	// Verify mount is read-only (common for config)
	require.True(t, mount.ReadOnly)
}

func TestMultipleVolumeMounts(t *testing.T) {
	t.Parallel()

	// Test creating multiple mounts for different volumes
	mounts := []corev1.VolumeMount{
		NewVolumeMount("config", "/etc/config", true),
		NewVolumeMount("data", "/data", false),
		NewVolumeMount("logs", "/var/log", false),
	}

	require.Len(t, mounts, 3)

	// Verify each mount is distinct
	names := make(map[string]bool)
	paths := make(map[string]bool)

	for _, mount := range mounts {
		require.NotEmpty(t, mount.Name)
		require.NotEmpty(t, mount.MountPath)

		// Check for duplicates
		require.False(t, names[mount.Name], "Volume names should be unique")
		require.False(t, paths[mount.MountPath], "Mount paths should be unique")

		names[mount.Name] = true
		paths[mount.MountPath] = true
	}
}

func TestVolumeFromConfigMapWithDifferentNames(t *testing.T) {
	t.Parallel()

	// Test that volume name can differ from configmap name
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "production-database-config",
		},
	}

	volume := NewVolumeFromConfigMap("db-config", configMap)

	require.Equal(t, "db-config", volume.Name,
		"Volume name should match the provided name parameter")
	require.Equal(t, "production-database-config", volume.VolumeSource.ConfigMap.Name,
		"ConfigMap name should be preserved")
}

func TestCommonVolumeMountPatterns(t *testing.T) {
	t.Parallel()

	// Test common patterns used in the operator

	t.Run("config volume (read-only)", func(t *testing.T) {
		t.Parallel()

		mount := NewVolumeMount("config", "/etc/config", true)
		require.True(t, mount.ReadOnly)
		require.Equal(t, "/etc/config", mount.MountPath)
	})

	t.Run("data volume (read-write)", func(t *testing.T) {
		t.Parallel()

		mount := NewVolumeMount("data", "/data", false)
		require.False(t, mount.ReadOnly)
		require.Equal(t, "/data", mount.MountPath)
	})

	t.Run("caddyfile volume (read-only)", func(t *testing.T) {
		t.Parallel()

		mount := NewVolumeMount("caddyfile", "/gateway", true)
		require.True(t, mount.ReadOnly)
		require.Equal(t, "/gateway", mount.MountPath)
	})
}