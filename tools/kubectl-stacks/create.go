package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func NewCreateCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	return &cobra.Command{
		Use:  "create <stack-name>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getRestClient(configFlags)
			if err != nil {
				return err
			}

			return create(cmd, client, args[0])
		},
	}
}

func create(cmd *cobra.Command, client *rest.RESTClient, name string) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Creating stack '%s'...\r\n", name)

	stack := &v1beta1.Stack{}
	stack.SetName(name)

	return client.Post().
		Resource("Stacks").
		Body(stack).
		Do(cmd.Context()).
		Error()
}
