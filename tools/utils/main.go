package main

import (
	"fmt"
	"os"

	"github.com/formancehq/operator/tools/utils/v3/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
