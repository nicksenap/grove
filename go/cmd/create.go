package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	createBranch      string
	createRepos       string
	createPreset      string
	createAll         bool
	createCopyClaudeMD *bool
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

			// Offer to save as preset
			if len(cfg.Presets) == 0 && console.IsTerminal(os.Stdin) {
				if console.Confirm("Save this selection as a preset?", false) {
					presetName := console.Prompt("Preset name")
					if presetName != "" {
						if cfg.Presets == nil {
							cfg.Presets = make(map[string]models.Preset)
						}
						cfg.Presets[presetName] = models.Preset{Repos: repoNames}
						if err := config.Save(cfg); err != nil {
							console.Warningf("Could not save preset: %s", err)
						} else {
							console.Successf("Saved preset %q", presetName)
						}
					}
				}
			}
		}

		// Validate repos exist
		for _, name := range repoNames {
			if _, ok := repoMap[name]; !ok {
				exitError("Unknown repo: " + name + ". Available: " + strings.Join(repoNamesList(repos), ", "))
			}
		}

		// Branch — prompt if omitted and in a terminal
		branch := createBranch
		if branch == "" {
			if console.IsTerminal(os.Stdin) {
				branch = console.Prompt("Branch name")
			}
			if branch == "" {
				exitError("Branch is required: --branch / -b")
			}
		}

		// Name
		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			name = deriveName(branch)
		}

		if err := workspace.NewService().Create(name, branch, repoNames, repoMap, cfg); err != nil {
			exitError(err.Error())
		}

		// Copy CLAUDE.md if requested
		wsPath := filepath.Join(cfg.WorkspaceDir, name)
		if createCopyClaudeMD != nil {
			if *createCopyClaudeMD {
				copyCLAUDEmd(repoMap, repoNames, wsPath)
			}
		} else if console.IsTerminal(os.Stdin) {
			// Auto-detect: check if any repo has a CLAUDE.md
			for _, repoName := range repoNames {
				if src, ok := repoMap[repoName]; ok {
					claudeMD := filepath.Join(src, "CLAUDE.md")
					if _, err := os.Stat(claudeMD); err == nil {
						if console.Confirm("Copy CLAUDE.md into workspace?", true) {
							copyCLAUDEmd(repoMap, repoNames, wsPath)
						}
						break
					}
				}
			}
		}
	},
}

func init() {
	createCmd.Flags().StringVarP(&createBranch, "branch", "b", "", "Branch name")
	createCmd.Flags().StringVarP(&createRepos, "repos", "r", "", "Comma-separated repo names")
	createCmd.Flags().StringVarP(&createPreset, "preset", "p", "", "Use named preset")
	createCmd.Flags().BoolVar(&createAll, "all", false, "Use all discovered repos")
	createCmd.Flags().Bool("copy-claude-md", false, "Copy CLAUDE.md into workspace dir")
	createCmd.Flags().Bool("no-copy-claude-md", false, "Don't copy CLAUDE.md")

	createCmd.RegisterFlagCompletionFunc("repos", completeRepoNames)
	createCmd.RegisterFlagCompletionFunc("preset", completePresetNames)

	// Resolve --copy-claude-md / --no-copy-claude-md
	createCmd.PreRun = func(cmd *cobra.Command, args []string) {
		if cmd.Flags().Changed("copy-claude-md") {
			v := true
			createCopyClaudeMD = &v
		} else if cmd.Flags().Changed("no-copy-claude-md") {
			v := false
			createCopyClaudeMD = &v
		}
	}
}

func copyCLAUDEmd(repoMap map[string]string, repoNames []string, wsPath string) {
	for _, repoName := range repoNames {
		src, ok := repoMap[repoName]
		if !ok {
			continue
		}
		claudeMD := filepath.Join(src, "CLAUDE.md")
		data, err := os.ReadFile(claudeMD)
		if err != nil {
			continue
		}
		dst := filepath.Join(wsPath, "CLAUDE.md")
		os.WriteFile(dst, data, 0o644)
		return // only copy from first repo that has it
	}
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

