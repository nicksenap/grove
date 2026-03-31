package cmd

import (
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	statusJSON    bool
	statusVerbose bool
)

var statusCmd = &cobra.Command{
	Use:   "status [NAME]",
	Short: "Show git status across workspace repos",
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

		if err := workspace.Status(ws.Name, statusJSON); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	statusCmd.Flags().BoolVarP(&statusJSON, "json", "j", false, "Output as JSON")
	statusCmd.Flags().BoolVarP(&statusVerbose, "verbose", "V", false, "Show full git status")
}
