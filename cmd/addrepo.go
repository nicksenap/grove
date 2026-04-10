package cmd

import (
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var addRepoRepos string

var addRepoCmd = &cobra.Command{
	Use:   "add-repo [NAME]",
	Short: "Add repos to an existing workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var wsName string
		if len(args) > 0 {
			wsName = args[0]
		} else {
			// Auto-detect workspace from cwd and use it as default.
			if cwd, err := os.Getwd(); err == nil {
				if currentWs, _ := state.FindWorkspaceByPath(cwd); currentWs != nil {
					wsName = currentWs.Name
					console.Infof("Using current workspace: %s", wsName)
				}
			}

			if wsName == "" {
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
		}

		cfg := config.RequireConfig()
		repos := discover.FindAllRepos(cfg.RepoDirs)
		repoMap := discover.RepoMap(repos)

		var repoNames []string
		if addRepoRepos != "" {
			repoNames = strings.Split(addRepoRepos, ",")
			for i := range repoNames {
				repoNames[i] = strings.TrimSpace(repoNames[i])
			}
			// Clone any remote URLs into the first repo_dir
			for i, name := range repoNames {
				if gitops.IsGitURL(name) {
					if len(cfg.RepoDirs) == 0 {
						exitError("No repo_dirs configured — cannot clone remote repo")
					}
					console.Infof("Cloning %s ...", name)
					clonedPath, err := gitops.Clone(name, cfg.RepoDirs[0])
					if err != nil {
						exitError(err.Error())
					}
					repoName := gitops.RepoNameFromURL(name)
					repoMap[repoName] = clonedPath
					repoNames[i] = repoName
					console.Successf("Cloned %s into %s", repoName, clonedPath)
				}
			}
		} else {
			// Interactive: show repos not already in workspace
			ws, err := state.GetWorkspace(wsName)
			if err != nil {
				exitError(err.Error())
			}
			if ws == nil {
				exitError("Workspace not found: " + wsName)
			}
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

		if err := workspace.NewService().AddRepos(wsName, repoNames, repoMap); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	addRepoCmd.Flags().StringVarP(&addRepoRepos, "repos", "r", "", "Comma-separated repo names or git URLs")
	addRepoCmd.RegisterFlagCompletionFunc("repos", completeRepoNames)
	addRepoCmd.ValidArgsFunction = completeWorkspaceNames
}
