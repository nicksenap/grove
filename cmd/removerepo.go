package cmd

import (
	"fmt"
	"strings"

	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	removeRepoRepos string
	removeRepoForce bool
)

var removeRepoCmd = &cobra.Command{
	Use:   "remove-repo [NAME]",
	Short: "Remove repos from a workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var wsName string
		if len(args) > 0 {
			wsName = args[0]
		} else {
			wsName = pickWorkspaceName("Select workspace:")
		}

		var repoNames []string
		if removeRepoRepos != "" {
			repoNames = parseRepoList(removeRepoRepos)
		} else {
			// Interactive: pick from repos in workspace
			ws, err := state.GetWorkspace(wsName)
			if err != nil {
				fail(err)
			}
			if ws == nil {
				fail(workspace.ErrWorkspaceNotFound(wsName))
			}
			if len(ws.Repos) == 0 {
				fail(machine.Errorf(machine.CodeRepoNotFound, "workspace %s has no repos", wsName))
			}
			choices := ws.RepoNames()
			selected, err := prompter.PickMany("Select repos to remove:", choices)
			if err != nil {
				exitOnPickerErr(err)
			}
			repoNames = selected
		}

		if !removeRepoForce {
			// Removing a repo deletes its worktree, so machine mode requires the
			// destructive intent to be explicit instead of assumed from a prompt
			// it is not allowed to show.
			requireArgs("--force", "gw remove-repo "+wsName+" -r <repos> --force --format json")
			if !prompter.Confirm(fmt.Sprintf("Remove %s from %s?", strings.Join(repoNames, ", "), wsName), false) {
				return
			}
		}

		result, err := workspace.NewService().RemoveRepos(wsName, repoNames)
		if err != nil {
			fail(err)
		}
		machine.Emit(result,
			machine.NextAction("Inspect repo state", "gw status "+wsName+" --format json"))
	},
}

func init() {
	removeRepoCmd.Flags().StringVarP(&removeRepoRepos, "repos", "r", "", "Comma-separated repo names")
	removeRepoCmd.Flags().BoolVarP(&removeRepoForce, "force", "f", false, "Skip confirmation")
	removeRepoCmd.RegisterFlagCompletionFunc("repos", completeRepoNames)
	removeRepoCmd.ValidArgsFunction = completeWorkspaceNames
}
