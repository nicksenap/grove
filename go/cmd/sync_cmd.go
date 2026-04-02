package cmd

import (
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync [NAME]",
	Short: "Sync workspace repos by rebasing onto base branches",
	Long:  "Auto-detects workspace from cwd if name omitted.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}

		ws, err := workspace.ResolveWorkspace(name)
		if err != nil {
			exitError(err.Error())
		}

		if err := workspace.Sync(ws.Name); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	syncCmd.ValidArgsFunction = completeWorkspaceNames
}
