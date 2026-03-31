package cmd

import (
	"strings"

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
		if len(args) == 0 {
			exitError("workspace name required")
		}
		if removeRepoRepos == "" {
			exitError("--repos is required")
		}

		repoNames := strings.Split(removeRepoRepos, ",")
		for i := range repoNames {
			repoNames[i] = strings.TrimSpace(repoNames[i])
		}

		// TODO: confirmation if not --force

		if err := workspace.RemoveRepos(args[0], repoNames); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	removeRepoCmd.Flags().StringVarP(&removeRepoRepos, "repos", "r", "", "Comma-separated repo names")
	removeRepoCmd.Flags().BoolVarP(&removeRepoForce, "force", "f", false, "Skip confirmation")
}
