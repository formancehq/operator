package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/formancehq/go-libs/v2/service"
)

var rootCmd = &cobra.Command{
	Use:   "utils",
	Short: "A cli for operator operations",
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

func Execute() {
	service.Execute(rootCmd)
}

func init() {
	rootCmd.AddCommand(NewDatabaseCommand())
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	service.AddFlags(rootCmd.PersistentFlags())
}
