package cli

import (
	"github.com/spf13/cobra"
)

func MakeRootCommand() *cobra.Command {
	var cmd = &cobra.Command{
		SilenceUsage: true,
		Use:          "shinzo",
		Short:        "Shinzo View Creator",
		Long:         ``,
	}

	return cmd
}
