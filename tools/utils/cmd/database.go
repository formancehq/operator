package cmd

import (
	"github.com/spf13/cobra"

	bunconnect "github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
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
