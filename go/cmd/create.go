package cmd

import (
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	createBranch string
	createRepos  string
	createPreset string
	createAll    bool
)

var createCmd = &cobra.Command{
	Use:   "create [NAME]",
	Short: "Create a new workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		repos := discover.FindAllRepos(cfg.RepoDirs)
		repoMap := discover.RepoMap(repos)

		var repoNames []string

		// Resolve repos from preset
		if createPreset != "" {
			preset, ok := cfg.Presets[createPreset]
			if !ok {
				exitError("Preset not found: " + createPreset)
			}
			repoNames = preset.Repos
		} else if createAll {
			for _, r := range repos {
				repoNames = append(repoNames, r.Name)
			}
		} else if createRepos != "" {
			repoNames = strings.Split(createRepos, ",")
			for i := range repoNames {
				repoNames[i] = strings.TrimSpace(repoNames[i])
			}
		} else {
			// Interactive
			choices := make([]string, len(repos))
			for i, r := range repos {
				choices[i] = r.Name
			}
			selected, err := picker.PickMany("Select repos for workspace:", choices)
			if err != nil {
				exitError(err.Error())
			}
			repoNames = selected
		}

		// Validate repos exist
		for _, name := range repoNames {
			if _, ok := repoMap[name]; !ok {
				exitError("Unknown repo: " + name + ". Available: " + strings.Join(repoNamesList(repos), ", "))
			}
		}

		// Branch
		branch := createBranch
		if branch == "" {
			exitError("Branch is required: --branch / -b")
		}

		// Name
		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			name = deriveName(branch)
		}

		if err := workspace.Create(name, branch, repoNames, repoMap, cfg); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	createCmd.Flags().StringVarP(&createBranch, "branch", "b", "", "Branch name")
	createCmd.Flags().StringVarP(&createRepos, "repos", "r", "", "Comma-separated repo names")
	createCmd.Flags().StringVarP(&createPreset, "preset", "p", "", "Use named preset")
	createCmd.Flags().BoolVar(&createAll, "all", false, "Use all discovered repos")
}

func deriveName(branch string) string {
	name := strings.ReplaceAll(branch, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.Trim(name, "-")
	return name
}

func repoNamesList(repos []discover.Repo) []string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return names
}

