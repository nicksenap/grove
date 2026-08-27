package cmd

import (
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var addRepoRepos string

var addRepoCmd = &cobra.Command{
	Use:   "add-repo [NAME]",
	Short: "Add repos to an existing workspace",
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

		cfg := config.RequireConfig()
		repos := discover.UniqueByName(discover.DiscoverReposWithCache(cfg.RepoDirs))
		repoMap := discover.RepoMap(repos)

		var repoNames []string
		if addRepoRepos != "" {
			repoNames = parseRepoFlag(addRepoRepos)
			// Clone any remote URLs into the first repo_dir
			for i, repoName := range repoNames {
				if gitops.IsGitURL(repoName) {
					if len(cfg.RepoDirs) == 0 {
						exitError("No repo_dirs configured — cannot clone remote repo")
					}
					console.Infof("Cloning %s ...", repoName)
					clonedPath, clonedName, err := gitops.Clone(repoName, cfg.RepoDirs[0])
					if err != nil {
						exitError(err.Error())
					}
					if existing, ok := repoMap[clonedName]; ok && existing != clonedPath {
						exitError("repo name conflict: " + clonedName + " already exists locally at " + existing)
					}
					repoMap[clonedName] = clonedPath
					repoNames[i] = clonedName
					console.Successf("Cloned %s into %s", clonedName, clonedPath)
				}
			}
		} else {
			existing := make(map[string]bool)
			for _, r := range ws.Repos {
				existing[r.RepoName] = true
			}
			var choices []string
			for _, r := range repos {
				if !existing[r.Name] {
					choices = append(choices, r.Name)
				}
			}
			if len(choices) == 0 {
				exitError("All discovered repos are already in the workspace")
			}
			selected, err := picker.PickMany("Select repos to add:", choices)
			if err != nil {
				exitOnPickerErr(err)
			}
			repoNames = selected
		}

		if err := workspace.NewService().AddRepos(ws.Name, repoNames, repoMap); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	addRepoCmd.Flags().StringVarP(&addRepoRepos, "repos", "r", "", "Comma-separated repo names or git URLs")
	addRepoCmd.RegisterFlagCompletionFunc("repos", completeAddRepoNames)
	addRepoCmd.ValidArgsFunction = completeWorkspaceNames
}

func parseRepoFlag(value string) []string {
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
