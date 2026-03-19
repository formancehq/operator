package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/formancehq/go-libs/v4/pointer"
)

func HashFromConfigMaps(configMaps ...*corev1.ConfigMap) string {
	digest := sha256.New()
	for _, configMap := range configMaps {
		if err := json.NewEncoder(digest).Encode(configMap.Data); err != nil {
			panic(err)
		}
	}
	return base64.StdEncoding.EncodeToString(digest.Sum(nil))
}

func HashFromResources(resources ...*unstructured.Unstructured) string {
	buf := bytes.NewBufferString("")
	for _, resource := range resources {
		buf.WriteString(string(resource.GetUID()))
		buf.WriteString(resource.GetResourceVersion())
	}
	digest := sha256.New()
	digest.Write(buf.Bytes())

	return base64.StdEncoding.EncodeToString(digest.Sum(nil))
}

func CopyDir(f fs.FS, root, path string, ret *map[string]string) {
	dirEntries, err := fs.ReadDir(f, path)
	if err != nil {
		panic(err)
	}
	for _, dirEntry := range dirEntries {
		dirEntryPath := filepath.Join(path, dirEntry.Name())
		if dirEntry.IsDir() {
			CopyDir(f, root, dirEntryPath, ret)
		} else {
			fileContent, err := fs.ReadFile(f, dirEntryPath)
			if err != nil {
				panic(err)
			}
			sanitizedPath := strings.TrimPrefix(dirEntryPath, root)
			sanitizedPath = strings.TrimPrefix(sanitizedPath, "/")
			(*ret)[sanitizedPath] = string(fileContent)
		}
	}
}

type ObjectMutator[T any] func(t T) error

func WithController[T client.Object](scheme *k8sruntime.Scheme, owner client.Object) ObjectMutator[T] {
	return func(t T) error {
		if !metav1.IsControlledBy(t, owner) {
			if err := controllerutil.SetControllerReference(owner, t, scheme); err != nil {
				return err
			}
		}
		return nil
	}
}

func WithOwner[T client.Object](scheme *k8sruntime.Scheme, owner client.Object) ObjectMutator[T] {
	return func(t T) error {
		if err := controllerutil.SetOwnerReference(owner, t, scheme); err != nil {
			panic(err)
		}

		return nil
	}
}

func WithAnnotations[T client.Object](newAnnotations map[string]string) ObjectMutator[T] {
	return func(t T) error {
		annotations := t.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		for k, v := range newAnnotations {
			annotations[k] = v
		}
		t.SetAnnotations(annotations)

		return nil
	}
}

func WithLabels[T client.Object](newLabels map[string]string) ObjectMutator[T] {
	return func(t T) error {
		annotations := t.GetLabels()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		for k, v := range newLabels {
			annotations[k] = v
		}
		t.SetLabels(annotations)

		return nil
	}
}

func CreateOrUpdate[T client.Object](ctx Context,
	key types.NamespacedName, mutators ...ObjectMutator[T]) (T, controllerutil.OperationResult, error) {

	var ret T
	ret = reflect.New(reflect.TypeOf(ret).Elem()).Interface().(T)
	ret.SetNamespace(key.Namespace)
	ret.SetName(key.Name)
	operationResult, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), ret, func() error {
		for _, mutate := range mutators {
			if err := mutate(ret); err != nil {
				return err
			}
		}
		return nil
	})
	return ret, operationResult, err
}

func DeleteIfExists[T client.Object](ctx Context, name types.NamespacedName) error {
	var t T
	t = reflect.New(reflect.TypeOf(t).Elem()).Interface().(T)
	if err := ctx.GetClient().Get(ctx, name, t); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		return err
	}
	LogDeletion(ctx, t, "DeleteIfExists")
	return ctx.GetClient().Delete(ctx, t)
}

func LogDeletion(ctx Context, obj client.Object, caller string) {
	logger := log.FromContext(ctx)
	gvk, _ := apiutil.GVKForObject(obj, ctx.GetScheme())
	// Capture caller stack for debugging unexpected deletions
	stack := make([]byte, 4096)
	stack = stack[:runtime.Stack(stack, false)]
	callerFrames := formatCallerFrames(stack)
	logger.Info(fmt.Sprintf("DELETE %s %s/%s (caller: %s)\n%s",
		gvk.Kind, obj.GetNamespace(), obj.GetName(), caller, callerFrames))
}

func formatCallerFrames(stack []byte) string {
	lines := strings.Split(string(stack), "\n")
	// Skip first 2 lines (goroutine header) and the LogDeletion frames,
	// keep up to 10 relevant frames
	var result []string
	skip := true
	for _, line := range lines {
		if skip && strings.Contains(line, "LogDeletion") {
			continue
		}
		if skip && !strings.Contains(line, "LogDeletion") && !strings.HasPrefix(line, "goroutine") {
			skip = false
		}
		if !skip {
			result = append(result, line)
			if len(result) >= 20 {
				break
			}
		}
	}
	return strings.Join(result, "\n")
}

func hasOwnerReference(ctx Context, owner client.Object, object client.Object, controller, blockOwnerDeletion bool) (bool, error) {
	kinds, _, err := ctx.GetScheme().ObjectKinds(owner)
	if err != nil {
		return false, nil
	}
	expectedOwnerReference := metav1.OwnerReference{
		APIVersion: kinds[0].GroupVersion().String(),
		Kind:       kinds[0].Kind,
		Name:       owner.GetName(),
		UID:        owner.GetUID(),
	}
	if controller {
		expectedOwnerReference.Controller = pointer.For(true)
	}
	if blockOwnerDeletion {
		expectedOwnerReference.BlockOwnerDeletion = pointer.For(true)
	}

	for _, reference := range object.GetOwnerReferences() {
		if equality.Semantic.DeepDerivative(expectedOwnerReference, reference) {
			return true, nil
		}
	}

	return false, nil
}

func HasControllerReference(ctx Context, owner client.Object, object client.Object) (bool, error) {
	return hasOwnerReference(ctx, owner, object, true, true)
}

func HasOwnerReference(ctx Context, owner client.Object, object client.Object) (bool, error) {
	return hasOwnerReference(ctx, owner, object, false, false)
}

// GetStackNameFromUnstructured safely extracts .spec.stack from an unstructured object.
func GetStackNameFromUnstructured(u *unstructured.Unstructured) (string, bool) {
	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		return "", false
	}
	stack, ok := spec["stack"].(string)
	return stack, ok
}

// GetSpecFromUnstructured safely extracts .spec as map[string]any.
func GetSpecFromUnstructured(u *unstructured.Unstructured) (map[string]any, bool) {
	spec, ok := u.Object["spec"].(map[string]any)
	return spec, ok
}
