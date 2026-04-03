package cmd

import (
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	statusJSON    bool
	statusVerbose bool
	statusPR      bool
	statusAll     bool
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

		if statusAll {
			console.Warning("--all is deprecated. Use: gw list -s")
		}

		ws, err := workspace.ResolveWorkspace(name)
		if err != nil {
			exitError(err.Error())
		}

		if err := workspace.NewService().Status(ws.Name, workspace.StatusOptions{
			JSON:    statusJSON,
			Verbose: statusVerbose,
			PR:      statusPR,
		}); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	statusCmd.Flags().BoolVarP(&statusJSON, "json", "j", false, "Output as JSON")
	statusCmd.Flags().BoolVarP(&statusVerbose, "verbose", "V", false, "Show full git status")
	statusCmd.Flags().BoolVarP(&statusPR, "pr", "P", false, "Show PR/MR status (requires gh or glab)")
	statusCmd.Flags().BoolVarP(&statusAll, "all", "a", false, "Show all workspaces (deprecated, use: gw list -s)")
	statusCmd.Flags().MarkDeprecated("all", "use 'gw list -s' instead")
	statusCmd.ValidArgsFunction = completeWorkspaceNames
}
