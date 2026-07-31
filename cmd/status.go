package cmd

import (
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/machine"
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
			fail(err)
		}

		opts := workspace.StatusOptions{
			JSON:    statusJSON,
			Verbose: statusVerbose,
			PR:      statusPR,
		}
		svc := workspace.NewService()

		if machine.Enabled() {
			report, err := svc.StatusReport(ws.Name, opts)
			if err != nil {
				fail(err)
			}
			machine.Emit(report, statusNextActions(report)...)
			return
		}

		if err := svc.Status(ws.Name, opts); err != nil {
			fail(err)
		}
	},
}

// statusNextActions suggests the follow-up that matches the observed state:
// rebase when a repo is behind, commit guidance when a repo is dirty. Both are
// omitted when neither applies, rather than padding the envelope with noise.
func statusNextActions(report *workspace.StatusReport) []machine.Action {
	var actions []machine.Action
	if report.Behind() {
		actions = append(actions, machine.NextAction("Rebase repos onto their base branches",
			"gw sync "+report.Workspace+" --format json"))
	}
	if dirty := report.Dirty(); len(dirty) > 0 {
		actions = append(actions, machine.NextAction("Review uncommitted changes in "+dirty[0],
			"git -C "+report.Path+"/"+dirty[0]+" diff"))
	}
	return actions
}

func init() {
	statusCmd.Flags().BoolVarP(&statusJSON, "json", "j", false, legacyJSONUsage)
	statusCmd.Flags().BoolVarP(&statusVerbose, "verbose", "V", false, "Show full git status")
	statusCmd.Flags().BoolVarP(&statusPR, "pr", "P", false, "Show PR/MR status (requires gh or glab)")
	statusCmd.Flags().BoolVarP(&statusAll, "all", "a", false, "Show all workspaces (deprecated, use: gw list -s)")
	statusCmd.Flags().MarkDeprecated("all", "use 'gw list -s' instead")
	statusCmd.ValidArgsFunction = completeWorkspaceNames
}
