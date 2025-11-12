package core

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

func GetObjectName(stack, name string) string {
	return fmt.Sprintf("%s-%s", stack, name)
}

func GetNamespacedResourceName(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
}

func GetResourceName(name string) types.NamespacedName {
	return types.NamespacedName{
		Name: name,
	}
}

// GetNamespaceName generates a namespace name with optional prefix
// Format: <prefix><organisationID>-<stackID> or <organisationID>-<stackID> if no prefix
func GetNamespaceName(platform Platform, stackName string) string {
	if platform.NamespacePrefix == "" {
		return stackName
	}
	return platform.NamespacePrefix + stackName
}

// GetNamespacedResourceNameWithPrefix generates a namespaced resource name using the prefixed namespace
func GetNamespacedResourceNameWithPrefix(platform Platform, stackName, name string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: GetNamespaceName(platform, stackName),
		Name:      name,
	}
}
