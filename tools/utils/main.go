package main

import (
	"fmt"
	"os"

	"github.com/formancehq/go-libs/v2/service"

	"github.com/formancehq/operator/tools/utils/v3/cmd"
)

func main() {
	service.BindEnvToCommand(cmd.RootCmd)
	if err := cmd.RootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
