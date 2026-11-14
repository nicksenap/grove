package cmd

import (
	"os"
	"sort"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/operations"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	createBranch        string
	createRepos         string
	createPreset        string
	createAll           bool
	createReplace       bool
	createForce         bool
	createTrack         bool
	createSourceURL     string
	createSourceProvide string
	createSourceRef     string
	createSourceTitle   string
)

var createCmd = &cobra.Command{
	Use:   "create [NAME]",
	Short: "Create a new workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		repos := discover.UniqueByName(discover.DiscoverReposWithCache(cfg.RepoDirs))
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
			// Clone any remote git URLs into the first repo_dir (mirrors add-repo).
			// This lets a resolver pass an unmatched repo as a clone URL.
			for i, name := range repoNames {
				if !gitops.IsGitURL(name) {
					continue
				}
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
		} else {
			repoNames = pickCreateRepos(cfg, repos)
		}

		// Validate repos exist
		for _, name := range repoNames {
			if _, ok := repoMap[name]; !ok {
				exitError("Unknown repo: " + name + ". Available: " + strings.Join(repoNamesList(repos), ", "))
			}
		}

		// Name — known early so the branch prompt can default to it.
		var name string
		if len(args) > 0 {
			name = args[0]
		}

		// Branch — prompt if omitted and in a terminal.
		branch := createBranch
		if branch == "" {
			if console.IsTerminal(os.Stdin) {
				branch = console.PromptDefault("Branch name", name)
			}
			if branch == "" {
				exitError("Branch is required: --branch / -b")
			}
		}
		if name == "" {
			name = deriveName(branch)
		}

		// Build provenance + branch-mode options. A source URL is opaque to core;
		// --track checks out an existing remote branch (e.g. a PR head) instead
		// of creating a new one, falling back to create-mode if it is missing.
		var source *models.WorkspaceSource
		if createSourceURL != "" || createSourceProvide != "" {
			source = &models.WorkspaceSource{
				Provider: createSourceProvide,
				URL:      createSourceURL,
				Ref:      createSourceRef,
				Title:    createSourceTitle,
			}
		}
		opts := workspace.CreateOpts{
			Branch:  branch,
			Repos:   repoNames,
			RepoMap: repoMap,
			Cfg:     cfg,
			Source:  source,
		}
		if createTrack {
			opts.BranchMode = workspace.BranchModeTrack
		}

		// Resolve the branch before --replace deletes anything, so a failing
		// resolve_branch hook leaves the current workspace untouched.
		svc := operations.NewService()
		req, err := svc.ResolveBranch(operations.CreateRequest{Name: name, Options: opts})
		if err != nil {
			exitError(err.Error())
		}

		// --replace: delete the current workspace (detected from cwd) before creating the new one.
		replacedName := ""
		if createReplace {
			cwd, err := os.Getwd()
			if err != nil {
				exitError("cannot determine working directory: " + err.Error())
			}
			currentWs, _ := state.FindWorkspaceByPath(cwd)
			if currentWs == nil {
				exitError("--replace requires running from inside an existing workspace")
			}
			if currentWs.Name == name {
				exitError("--replace would collide: new workspace name matches the current one (" + name + "). Pass a different NAME.")
			}
			if !createForce {
				if !console.Confirm("Delete workspace "+currentWs.Name+" and replace with "+name+"?", false) {
					return
				}
			}
			console.Infof("Replacing workspace: deleting %s", currentWs.Name)
			if _, err := svc.Delete(operations.DeleteRequest{Name: currentWs.Name}); err != nil {
				exitError("failed to delete current workspace: " + err.Error())
			}
			replacedName = currentWs.Name
		}

		result, err := svc.Create(req)
		if result.Created && result.Workspace.Branch != branch {
			console.Infof("Branch: %s (from resolve_branch)", result.Workspace.Branch)
		}
		if err != nil {
			if replacedName != "" {
				exitError("failed to create new workspace (old workspace " + replacedName + " was already deleted): " + err.Error())
			}
			exitError(err.Error())
		}
	},
}

func init() {
	createCmd.Flags().StringVarP(&createBranch, "branch", "b", "", "Branch name")
	createCmd.Flags().StringVarP(&createRepos, "repos", "r", "", "Comma-separated repo names")
	createCmd.Flags().StringVarP(&createPreset, "preset", "p", "", "Use named preset")
	createCmd.Flags().BoolVar(&createAll, "all", false, "Use all discovered repos")
	createCmd.Flags().BoolVar(&createReplace, "replace", false, "Delete the current workspace (detected from cwd) before creating the new one")
	createCmd.Flags().BoolVarP(&createForce, "force", "f", false, "Skip --replace confirmation prompt")
	createCmd.Flags().BoolVar(&createTrack, "track", false, "Check out an existing remote branch (e.g. a PR head) instead of creating a new one")
	createCmd.Flags().StringVar(&createSourceURL, "source-url", "", "Record the source URL this workspace was seeded from")
	createCmd.Flags().StringVar(&createSourceProvide, "source-provider", "", "Source provider label (e.g. github, notion, slack)")
	createCmd.Flags().StringVar(&createSourceRef, "source-ref", "", "Source ref (PR number, page id, message ts)")
	createCmd.Flags().StringVar(&createSourceTitle, "source-title", "", "Human-readable source title for display")

	createCmd.RegisterFlagCompletionFunc("repos", completeRepoNames)
	createCmd.RegisterFlagCompletionFunc("preset", completePresetNames)
}

const (
	presetsTabTitle = "Presets"
	latestTabTitle  = "Latest"
	allTabTitle     = "All repos"
	allReposChoice  = "(all)"
	// latestTabLimit caps the Latest tab so it stays shorter than All repos.
	latestTabLimit = 10
)

// pickCreateRepos asks the user which repos the new workspace gets.
func pickCreateRepos(cfg *models.Config, repos []discover.RepoInfo) []string {
	all := repoNamesList(repos)
	recent, _ := stats.NewTracker(config.GroveDir).RecentRepos(0)
	tabs, presetByChoice := buildCreateTabs(cfg.Presets, recent, all)

	tab, selected, err := picker.PickTabs("Select repos for workspace:", tabs)
	if err != nil {
		exitOnPickerErr(err)
	}
	if tabs[tab].Title == presetsTabTitle {
		return cfg.Presets[presetByChoice[selected[0]]].Repos
	}
	for _, s := range selected {
		if s == allReposChoice {
			return all
		}
	}
	if len(cfg.Presets) == 0 && len(selected) < len(all) {
		offerSavePreset(cfg, selected)
	}
	return selected
}

// buildCreateTabs builds the tabs of the create picker: saved presets,
// recently used repos (newest first), and every discovered repo. Empty tabs
// are left out. presetByChoice maps a Presets row back to its preset name.
func buildCreateTabs(presets map[string]models.Preset, recent, all []string) (tabs []picker.Tab, presetByChoice map[string]string) {
	presetByChoice = make(map[string]string, len(presets))
	if len(presets) > 0 {
		names := make([]string, 0, len(presets))
		for name := range presets {
			names = append(names, name)
		}
		sort.Strings(names)
		choices := make([]string, len(names))
		for i, name := range names {
			choices[i] = name + "  (" + strings.Join(presets[name].Repos, ", ") + ")"
			presetByChoice[choices[i]] = name
		}
		tabs = append(tabs, picker.Tab{Title: presetsTabTitle, Choices: choices})
	}

	known := make(map[string]bool, len(all))
	for _, name := range all {
		known[name] = true
	}
	var latest []string
	for _, name := range recent {
		if known[name] && len(latest) < latestTabLimit {
			latest = append(latest, name)
		}
	}
	if len(latest) > 0 {
		tabs = append(tabs, picker.Tab{Title: latestTabTitle, Choices: latest, Multi: true})
	}

	if len(all) > 0 {
		choices := append([]string{allReposChoice}, all...)
		tabs = append(tabs, picker.Tab{Title: allTabTitle, Choices: choices, Multi: true})
	}
	return tabs, presetByChoice
}

// offerSavePreset asks whether to save the picked repos as the first preset.
func offerSavePreset(cfg *models.Config, repoNames []string) {
	if !console.IsTerminal(os.Stdin) || !console.Confirm("Save this selection as a preset?", false) {
		return
	}
	presetName := console.Prompt("Preset name")
	if presetName == "" {
		return
	}
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

func deriveName(branch string) string {
	name := strings.ReplaceAll(branch, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.Trim(name, "-")
	return name
}

func repoNamesList(repos []discover.RepoInfo) []string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return names
}
