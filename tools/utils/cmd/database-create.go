package cmd

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	bunconnect "github.com/formancehq/go-libs/v5/pkg/storage/bun/connect"
	bunmigrate "github.com/formancehq/go-libs/v5/pkg/storage/bun/migrate"
)

func NewDatabaseCreateCommand() *cobra.Command {
	ret := &cobra.Command{
		Use:   "create",
		Short: "Handle database creation",
		RunE: func(cmd *cobra.Command, args []string) error {
			connectionOptions, err := bunconnect.ConnectionOptionsFromFlags(cmd.Flags(), cmd.Context())
			if err != nil {
				return errors.Wrap(err, "resolving connection params")
			}

			return errors.Wrap(
				bunmigrate.EnsureDatabaseExists(cmd.Context(), *connectionOptions),
				"ensuring database exists",
			)
		},
	}
	return ret
}
