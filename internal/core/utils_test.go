package core

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestHashFromConfigMapsIsStableAndOrderSensitive(t *testing.T) {
	t.Parallel()

	first := &corev1.ConfigMap{Data: map[string]string{"a": "1"}}
	second := &corev1.ConfigMap{Data: map[string]string{"b": "2"}}

	require.Equal(t, HashFromConfigMaps(first, second), HashFromConfigMaps(first, second))
	require.NotEqual(t, HashFromConfigMaps(first, second), HashFromConfigMaps(second, first))
}

func TestHashFromResourcesUsesUIDAndResourceVersion(t *testing.T) {
	t.Parallel()

	resource := &unstructured.Unstructured{}
	resource.SetUID("uid-1")
	resource.SetResourceVersion("rv-1")

	same := &unstructured.Unstructured{}
	same.SetUID("uid-1")
	same.SetResourceVersion("rv-1")

	changed := &unstructured.Unstructured{}
	changed.SetUID("uid-1")
	changed.SetResourceVersion("rv-2")

	require.Equal(t, HashFromResources(resource), HashFromResources(same))
	require.NotEqual(t, HashFromResources(resource), HashFromResources(changed))
}

func TestCopyDirCopiesRelativeFileNames(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"root/a.txt":        {Data: []byte("a")},
		"root/nested/b.txt": {Data: []byte("b")},
	}
	ret := map[string]string{}

	CopyDir(files, "root", "root", &ret)

	require.Equal(t, map[string]string{
		"a.txt":        "a",
		"nested/b.txt": "b",
	}, ret)
}

func TestObjectMutators(t *testing.T) {
	t.Parallel()

	cm := &corev1.ConfigMap{}
	require.NoError(t, WithAnnotations[*corev1.ConfigMap](map[string]string{"a": "1"})(cm))
	require.NoError(t, WithLabels[*corev1.ConfigMap](map[string]string{"app": "operator"})(cm))

	require.Equal(t, map[string]string{"a": "1"}, cm.Annotations)
	require.Equal(t, map[string]string{"app": "operator"}, cm.Labels)
}

func TestUnstructuredSpecHelpers(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"stack": "stack0",
			"debug": true,
		},
	}}

	stack, ok := GetStackNameFromUnstructured(obj)
	require.True(t, ok)
	require.Equal(t, "stack0", stack)

	spec, ok := GetSpecFromUnstructured(obj)
	require.True(t, ok)
	require.Equal(t, true, spec["debug"])

	empty := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{}}}
	_, ok = GetStackNameFromUnstructured(empty)
	require.False(t, ok)
	_, ok = GetSpecFromUnstructured(empty)
	require.False(t, ok)
}

func TestFormatCallerFramesTrimsLogDeletionFrame(t *testing.T) {
	t.Parallel()

	stack := []byte("goroutine 1 [running]:\ninternal/core.LogDeletion()\n\tutils.go:1\ncaller.one()\n\tone.go:2\ncaller.two()\n\ttwo.go:3\n")

	require.Equal(t, "\tutils.go:1\ncaller.one()\n\tone.go:2\ncaller.two()\n\ttwo.go:3\n", formatCallerFrames(stack))
}
