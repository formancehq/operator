package cmd

import (
	"github.com/formancehq/go-libs/v2/bun/bunconnect"
	"github.com/formancehq/go-libs/v2/bun/bunmigrate"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func NewDatabaseDropCommand() *cobra.Command {
	ret := &cobra.Command{
		Use:   "drop",
		Short: "Handle database dropping",
		RunE: func(cmd *cobra.Command, args []string) error {
			connectionOptions, err := bunconnect.ConnectionOptionsFromFlags(cmd)
			if err != nil {
				return errors.Wrap(err, "resolving connection params")
			}

			return errors.Wrap(
				bunmigrate.EnsureDatabaseNotExists(cmd.Context(), *connectionOptions),
				"ensuring database does not exists",
			)
		},
	}
	return ret
}
