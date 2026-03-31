package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

// moduleCRD holds the kind and plural resource name extracted from a CRD.
type moduleCRD struct {
	Kind     string
	Plural   string
}

// discoverModules lists CRDs with label formance.com/kind=module and returns
// the available module kinds with their plural resource names.
func discoverModules(ctx context.Context, configFlags *genericclioptions.ConfigFlags) ([]moduleCRD, error) {
	restConfig, err := configFlags.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	restConfig.APIPath = "/apis"
	restConfig.GroupVersion = &apiExtensionsGV
	restConfig.NegotiatedSerializer = unstructuredNegotiator{}

	client, err := rest.RESTClientFor(restConfig)
	if err != nil {
		return nil, err
	}

	raw, err := client.Get().
		Resource("customresourcedefinitions").
		Param("labelSelector", "formance.com/kind=module").
		Do(ctx).
		Raw()
	if err != nil {
		return nil, fmt.Errorf("failed to list CRDs: %w", err)
	}

	var crdList struct {
		Items []struct {
			Spec struct {
				Names struct {
					Kind   string `json:"kind"`
					Plural string `json:"plural"`
				} `json:"names"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &crdList); err != nil {
		return nil, fmt.Errorf("failed to parse CRD list: %w", err)
	}

	modules := make([]moduleCRD, 0, len(crdList.Items))
	for _, item := range crdList.Items {
		modules = append(modules, moduleCRD{
			Kind:   item.Spec.Names.Kind,
			Plural: item.Spec.Names.Plural,
		})
	}

	return modules, nil
}

// resolveModule does a case-insensitive lookup of the input against discovered modules.
func resolveModule(input string, modules []moduleCRD) (moduleCRD, error) {
	lower := strings.ToLower(input)
	for _, m := range modules {
		if strings.ToLower(m.Kind) == lower {
			return m, nil
		}
	}
	kinds := make([]string, len(modules))
	for i, m := range modules {
		kinds[i] = m.Kind
	}
	return moduleCRD{}, fmt.Errorf("unknown module %q, available modules: %v", input, kinds)
}

func NewEnableModuleCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "enable-module <stack-name> <module>",
		Short: "Enable a module on a stack",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getRestClient(configFlags)
			if err != nil {
				return err
			}

			modules, err := discoverModules(cmd.Context(), configFlags)
			if err != nil {
				return err
			}

			return enableModule(cmd, client, modules, args[0], args[1])
		},
	}
}

func enableModule(cmd *cobra.Command, client *rest.RESTClient, modules []moduleCRD, stackName, moduleInput string) error {
	mod, err := resolveModule(moduleInput, modules)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Enabling module '%s' on stack '%s'...\r\n", mod.Kind, stackName)

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": v1beta1.GroupVersion.String(),
			"kind":       mod.Kind,
			"metadata": map[string]any{
				"name": stackName + "-" + toLowerKebab(mod.Kind),
			},
			"spec": map[string]any{
				"stack": stackName,
			},
		},
	}

	return client.Post().
		Resource(mod.Plural).
		Body(obj).
		Do(cmd.Context()).
		Error()
}

func toLowerKebab(s string) string {
	var result []byte
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result = append(result, '-')
			}
			result = append(result, byte(c)+32)
		} else {
			result = append(result, byte(c))
		}
	}
	return string(result)
}
