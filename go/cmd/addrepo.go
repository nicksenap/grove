package cmd

import (
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var addRepoRepos string

var addRepoCmd = &cobra.Command{
	Use:   "add-repo [NAME]",
	Short: "Add repos to an existing workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			exitError("workspace name required")
		}
		if addRepoRepos == "" {
			exitError("--repos is required")
		}

		cfg := config.RequireConfig()
		repos := discover.FindAllRepos(cfg.RepoDirs)
		repoMap := discover.RepoMap(repos)

		repoNames := strings.Split(addRepoRepos, ",")
		for i := range repoNames {
			repoNames[i] = strings.TrimSpace(repoNames[i])
		}

		if err := workspace.AddRepos(args[0], repoNames, repoMap); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	addRepoCmd.Flags().StringVarP(&addRepoRepos, "repos", "r", "", "Comma-separated repo names")
}
