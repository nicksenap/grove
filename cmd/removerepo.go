package cmd

import (
	"fmt"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/picker"
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

		var repoNames []string
		if removeRepoRepos != "" {
			repoNames = parseRepoFlag(removeRepoRepos)
		} else {
			if len(ws.Repos) == 0 {
				exitError("No repos in workspace")
			}
			selected, err := picker.PickMany("Select repos to remove:", ws.RepoNames())
			if err != nil {
				exitOnPickerErr(err)
			}
			repoNames = selected
		}

		if !removeRepoForce {
			if !console.Confirm(fmt.Sprintf("Remove %s from %s?", strings.Join(repoNames, ", "), ws.Name), false) {
				return
			}
		}

		if err := workspace.NewService().RemoveReposWithOptions(ws.Name, repoNames, workspace.RemoveOptions{Force: removeRepoForce}); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	removeRepoCmd.Flags().StringVarP(&removeRepoRepos, "repos", "r", "", "Comma-separated repo names")
	removeRepoCmd.Flags().BoolVarP(&removeRepoForce, "force", "f", false, "Skip worktree safety checks and confirmation")
	removeRepoCmd.RegisterFlagCompletionFunc("repos", completeRemoveRepoNames)
	removeRepoCmd.ValidArgsFunction = completeWorkspaceNames
}
