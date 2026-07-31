package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/lifecycle"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
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
		repos := discover.FindAllRepos(cfg.RepoDirs)
		repoMap := discover.RepoMap(repos)

		var repoNames []string

		// Resolve repos from preset
		if createPreset != "" {
			preset, ok := cfg.Presets[createPreset]
			if !ok {
				fail(machine.Errorf(machine.CodeUsage, "preset %s not found", createPreset).
					WithActions(machine.NextAction("List presets", "gw preset list --format json")))
			}
			repoNames = preset.Repos
		} else if createAll {
			for _, r := range repos {
				repoNames = append(repoNames, r.Name)
			}
		} else if createRepos != "" {
			repoNames = parseRepoList(createRepos)
			// Clone any remote git URLs into the first repo_dir (mirrors add-repo).
			// This lets a resolver pass an unmatched repo as a clone URL.
			for i, name := range repoNames {
				if !gitops.IsGitURL(name) {
					continue
				}
				if len(cfg.RepoDirs) == 0 {
					fail(machine.Errorf(machine.CodeNotInitialized, "no repo_dirs configured — cannot clone %s", name).
						WithActions(machine.NextAction("Add a repo directory", "gw add-dir <path>")))
				}
				console.Infof("Cloning %s ...", name)
				clonedPath, repoName, err := gitops.Clone(name, cfg.RepoDirs[0])
				if err != nil {
					fail(machine.Wrap(machine.CodeTransient, err, "cloning %s: %s", name, err).
						WithFix("Check network access and repository permissions, then retry"))
				}
				if existing, ok := repoMap[repoName]; ok && existing != clonedPath {
					fail(machine.Errorf(machine.CodeBranchConflict,
						"repo name conflict: %s already exists locally at %s", repoName, existing))
				}
				repoMap[repoName] = clonedPath
				repoNames[i] = repoName
				console.Successf("Cloned %s into %s", repoName, clonedPath)
			}
		} else {
			// Interactive
			repoChoices := make([]string, len(repos))
			for i, r := range repos {
				repoChoices[i] = r.Name
			}

			// If presets exist, offer them first with a "Pick manually..." escape hatch
			if len(cfg.Presets) > 0 {
				presetNames := make([]string, 0, len(cfg.Presets))
				presetChoices := make([]string, 0, len(cfg.Presets))
				for name, p := range cfg.Presets {
					presetNames = append(presetNames, name)
					presetChoices = append(presetChoices, name+"  ("+strings.Join(p.Repos, ", ")+")")
				}
				presetChoices = append(presetChoices, "Pick manually…")

				choice, err := picker.PickOne("Select repos from:", presetChoices)
				if err != nil {
					exitOnPickerErr(err)
				}

				if choice != "Pick manually…" {
					// Extract preset name (before the double space)
					for i, display := range presetChoices {
						if display == choice && i < len(presetNames) {
							repoNames = cfg.Presets[presetNames[i]].Repos
							break
						}
					}
				} else {
					selected, err := picker.PickMany("Select repos for workspace:", repoChoices)
					if err != nil {
						exitOnPickerErr(err)
					}
					repoNames = selected
				}
			} else {
				selected, err := picker.PickMany("Select repos for workspace:", repoChoices)
				if err != nil {
					exitOnPickerErr(err)
				}
				repoNames = selected

				// Offer to save as preset when none exist yet
				if console.IsTerminal(os.Stdin) && len(selected) < len(repos) {
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
		}

		// Validate repos exist
		for _, name := range repoNames {
			if _, ok := repoMap[name]; !ok {
				fail(workspace.ErrRepoNotFound(name).
					WithDetails(map[string]any{"available": repoNamesList(repos)}))
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
			requireArgs("--branch", "gw create "+name+" -b feat/x --format json")
			if console.IsTerminal(os.Stdin) {
				branch = console.PromptDefault("Branch name", name)
			}
			if branch == "" {
				fail(machine.Errorf(machine.CodeUsage, "branch is required").
					WithFix("Pass --branch / -b"))
			}
		}
		if name == "" {
			name = deriveName(branch)
		}

		// --replace: delete the current workspace (detected from cwd) before creating the new one.
		replacedName := ""
		if createReplace {
			cwd, err := os.Getwd()
			if err != nil {
				fail(machine.Wrap(machine.CodeInternal, err, "cannot determine working directory: %s", err))
			}
			currentWs, _ := state.FindWorkspaceByPath(cwd)
			if currentWs == nil {
				fail(machine.Errorf(machine.CodeUsage,
					"--replace requires running from inside an existing workspace").
					WithActions(machine.NextAction("Discover current context", "gw context --format json")))
			}
			if currentWs.Name == name {
				fail(machine.Errorf(machine.CodeWorkspaceExists,
					"--replace would collide: new workspace name matches the current one (%s)", name).
					WithFix("Pass a different NAME"))
			}
			// --replace deletes an existing workspace, so machine mode demands the
			// destructive intent be explicit rather than inferred from a skipped
			// prompt.
			if !createForce {
				requireArgs("--force (with --replace)", "gw create "+name+" -b <branch> --replace --force --format json")
				if !console.Confirm("Delete workspace "+currentWs.Name+" and replace with "+name+"?", false) {
					return
				}
			}
			console.Infof("Replacing workspace: deleting %s", currentWs.Name)
			vars := lifecycle.Vars{Name: currentWs.Name, Path: currentWs.Path, Branch: currentWs.Branch}
			if err := lifecycle.Run("pre_delete", vars); err != nil && !errors.Is(err, lifecycle.ErrNoHook) {
				if lifecycle.ShouldAbort(err) {
					fail(machine.Wrap(machine.CodeHookFailed, err, "%s", err))
				}
				console.Warning(err.Error())
			}
			if _, err := workspace.NewService().Delete(currentWs.Name); err != nil {
				fail(machine.Wrap(machine.CodeFor(err), err, "failed to delete current workspace: %s", err))
			}
			replacedName = currentWs.Name
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

		result, err := workspace.NewService().CreateWithOpts(name, opts)
		if err != nil {
			if replacedName != "" {
				// The replaced workspace is already gone, so this is not a no-op
				// failure — say so explicitly instead of leaving the caller to
				// assume nothing changed.
				fail(machine.Wrap(machine.CodeFor(err), err,
					"failed to create new workspace (old workspace %s was already deleted): %s", replacedName, err).
					WithDetails(map[string]any{"deleted_workspace": replacedName}))
			}
			fail(err)
		}
		result.Replaced = replacedName

		// Fire post_create hook if configured
		wsPath := filepath.Join(cfg.WorkspaceDir, name)
		vars := lifecycle.Vars{Name: name, Path: wsPath, Branch: branch}
		if source != nil {
			vars.SourceURL = source.URL
			vars.SourceRef = source.Ref
			vars.SourceTitle = source.Title
		}
		if err := lifecycle.Run("post_create", vars); err != nil && !errors.Is(err, lifecycle.ErrNoHook) {
			if lifecycle.ShouldAbort(err) {
				// The workspace exists; the hook is what failed. Report the
				// workspace in details so the caller does not retry create.
				fail(machine.Wrap(machine.CodeHookFailed, err, "%s", err).
					WithDetails(map[string]any{"workspace": name, "path": wsPath}).
					WithFix("Fix the post_create hook, or re-run with --no-hooks"))
			}
			console.Warning(err.Error())
		}

		machine.Emit(result,
			machine.NextAction("Inspect repo state", "gw status "+name+" --format json"),
			machine.NextAction("Run configured processes", "gw run "+name+" --format json"),
		)
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
