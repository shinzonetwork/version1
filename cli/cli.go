package cli

import (
	"github.com/spf13/cobra"
)

func NewShinzoCommand() *cobra.Command {
	view := MakeViewCommand()
	view.AddCommand(MakeViewCreateCommand())

	root := MakeRootCommand()
	root.AddCommand(
		view,
	)

	return root
}
