package cmd

import (
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var renameTo string

var renameCmd = &cobra.Command{
	Use:   "rename [NAME]",
	Short: "Rename a workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			name = pickWorkspaceName("Select workspace to rename:")
		}

		if renameTo == "" {
			exitError("--to is required")
		}

		if err := workspace.NewService().Rename(name, renameTo); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	renameCmd.Flags().StringVar(&renameTo, "to", "", "New workspace name")
	renameCmd.ValidArgsFunction = completeWorkspaceNames
}
