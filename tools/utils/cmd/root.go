package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/formancehq/go-libs/v2/service"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var RootCmd = &cobra.Command{
	Use:     "utils",
	Short:   "A cli for operator operations",
	Version: Version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		logger := logging.NewDefaultLogger(cmd.OutOrStdout(), service.IsDebug(cmd), false, false)
		logger.Infof("Starting application")
		logger.Debugf("Environment variables:")
		for _, v := range os.Environ() {
			logger.Debugf(v)
		}
		cmd.SetContext(logging.ContextWithLogger(cmd.Context(), logger))
		return nil
	},
}

func init() {
	RootCmd.AddCommand(NewDatabaseCommand())
	RootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	service.AddFlags(RootCmd.PersistentFlags())
}
