package cmd

import (
	"os"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/machine"
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
				wsName = pickWorkspaceName("Select workspace:")
			}
		}

		cfg := config.RequireConfig()
		repos := discover.FindAllRepos(cfg.RepoDirs)
		repoMap := discover.RepoMap(repos)

		var repoNames []string
		if addRepoRepos != "" {
			repoNames = parseRepoList(addRepoRepos)
			// Clone any remote URLs into the first repo_dir
			for i, name := range repoNames {
				if gitops.IsGitURL(name) {
					if len(cfg.RepoDirs) == 0 {
						exitError("No repo_dirs configured — cannot clone remote repo")
					}
					console.Infof("Cloning %s ...", name)
					clonedPath, repoName, err := gitops.Clone(name, cfg.RepoDirs[0])
					if err != nil {
						exitError(err.Error())
					}
					if existing, ok := repoMap[repoName]; ok && existing != clonedPath {
						exitError("repo name conflict: " + repoName + " already exists locally at " + existing)
					}
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
				fail(machine.Errorf(machine.CodeUsage,
					"all discovered repos are already in the workspace — nothing to add").
					WithActions(machine.NextAction("Discover more repo directories", "gw add-dir <path>")))
			}
			selected, err := picker.PickMany("Select repos to add:", choices)
			if err != nil {
				exitOnPickerErr(err)
			}
			repoNames = selected
		}

		result, err := workspace.NewService().AddRepos(wsName, repoNames, repoMap)
		if err != nil {
			fail(err)
		}
		machine.Emit(result,
			machine.NextAction("Inspect repo state", "gw status "+wsName+" --format json"))
	},
}

func init() {
	addRepoCmd.Flags().StringVarP(&addRepoRepos, "repos", "r", "", "Comma-separated repo names or git URLs")
	addRepoCmd.RegisterFlagCompletionFunc("repos", completeRepoNames)
	addRepoCmd.ValidArgsFunction = completeWorkspaceNames
}
