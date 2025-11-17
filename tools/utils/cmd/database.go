package cmd

import (
	"github.com/spf13/cobra"

	"github.com/formancehq/go-libs/v2/bun/bunconnect"
)

func NewDatabaseCommand() *cobra.Command {
	ret := &cobra.Command{
		Use:   "db",
		Short: "Handle databases operations",
	}
	ret.AddCommand(NewDatabaseCreateCommand(), NewDatabaseDropCommand())
	bunconnect.AddFlags(ret.PersistentFlags())

	return ret
}
