package cmd

import (
	"github.com/nicksenap/grove/internal/machine"
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
			fail(err)
		}

		result, err := workspace.NewService().Sync(ws.Name)
		if err != nil {
			fail(err)
		}

		// A repo that could not be rebased is reported in the result, not as a
		// command failure: sibling repos may have advanced, and the caller needs
		// to see which did.
		machine.Emit(result, syncNextActions(result)...)
	},
}

func syncNextActions(result *workspace.SyncResult) []machine.Action {
	var actions []machine.Action
	if failed := workspace.FailedRepos(result.Repos); len(failed) > 0 {
		actions = append(actions, machine.NextAction(
			"Inspect the repos that could not be rebased",
			"gw status "+result.Workspace+" --format json"))
	}
	for _, r := range result.Repos {
		if r.Outcome == workspace.OutcomeSkipped && r.Detail == "dirty working tree" {
			actions = append(actions, machine.NextAction(
				"Commit or stash changes in "+r.Repo+", then re-run sync",
				"git -C "+r.Path+" status"))
			break
		}
	}
	return actions
}

func init() {
	syncCmd.ValidArgsFunction = completeWorkspaceNames
}
