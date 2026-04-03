package cmd

import (
	"fmt"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/picker"
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
			workspaces, err := state.Load()
			if err != nil {
				exitError(err.Error())
			}
			if len(workspaces) == 0 {
				exitError("No workspaces")
			}
			choices := make([]string, len(workspaces))
			for i, ws := range workspaces {
				choices[i] = ws.Name
			}
			selected, err := picker.PickOne("Select workspace:", choices)
			if err != nil {
				exitOnPickerErr(err)
			}
			wsName = selected
		}

		var repoNames []string
		if removeRepoRepos != "" {
			repoNames = strings.Split(removeRepoRepos, ",")
			for i := range repoNames {
				repoNames[i] = strings.TrimSpace(repoNames[i])
			}
		} else {
			// Interactive: pick from repos in workspace
			ws, err := state.GetWorkspace(wsName)
			if err != nil {
				exitError(err.Error())
			}
			if ws == nil {
				exitError("Workspace not found: " + wsName)
			}
			if len(ws.Repos) == 0 {
				exitError("No repos in workspace")
			}
			choices := ws.RepoNames()
			selected, err := picker.PickMany("Select repos to remove:", choices)
			if err != nil {
				exitOnPickerErr(err)
			}
			repoNames = selected
		}

		if !removeRepoForce {
			if !console.Confirm(fmt.Sprintf("Remove %s from %s?", strings.Join(repoNames, ", "), wsName), false) {
				return
			}
		}

		if err := workspace.NewService().RemoveRepos(wsName, repoNames); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	removeRepoCmd.Flags().StringVarP(&removeRepoRepos, "repos", "r", "", "Comma-separated repo names")
	removeRepoCmd.Flags().BoolVarP(&removeRepoForce, "force", "f", false, "Skip confirmation")
	removeRepoCmd.RegisterFlagCompletionFunc("repos", completeRepoNames)
	removeRepoCmd.ValidArgsFunction = completeWorkspaceNames
}
