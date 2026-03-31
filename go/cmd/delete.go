package cmd

import (
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:   "delete [NAME]",
	Short: "Delete a workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			exitError("workspace name required")
		}

		name := args[0]

		// TODO: interactive confirmation if not --force

		if err := workspace.Delete(name); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&deleteForce, "force", "f", false, "Skip confirmation")
}
