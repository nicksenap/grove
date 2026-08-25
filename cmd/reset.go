package cmd

import (
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var resetDiscard bool

var resetCmd = &cobra.Command{
	Use:   "reset [NAME]",
	Short: "Switch workspace repos back to their workspace branch, then sync",
	Long:  "Auto-detects workspace from cwd if name omitted. Dirty repos on a different branch are skipped unless --discard is set.",
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

		if err := workspace.NewService().Reset(ws.Name, resetDiscard); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	resetCmd.Flags().BoolVar(&resetDiscard, "discard", false, "Discard tracked local changes so a dirty repo can switch")
	resetCmd.ValidArgsFunction = completeWorkspaceNames
}
