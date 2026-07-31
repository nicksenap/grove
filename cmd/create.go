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

// pickManuallyChoice is the escape hatch appended to the preset picker.
const pickManuallyChoice = "Pick manually…"

var createCmd = &cobra.Command{
	Use:   "create [NAME]",
	Short: "Create a new workspace",
	Args:  cobra.MaximumNArgs(1),
	// The steps are ordered, and the order is load-bearing: repos are resolved
	// (and possibly cloned) before validation, the branch is resolved before the
	// name can be derived from it, and --replace runs after the new name is known
	// but before anything is created — so a collision is caught before the old
	// workspace is destroyed.
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		repos := discover.FindAllRepos(cfg.RepoDirs)
		repoMap := discover.RepoMap(repos)

		repoNames := resolveCreateRepos(cfg, repos, repoMap)
		requireKnownRepos(repoNames, repoMap, repos)

		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		branch := resolveCreateBranch(name)
		if name == "" {
			name = deriveName(branch)
		}

		replacedName := replaceCurrentWorkspace(name)

		result, err := workspace.NewService().CreateWithOpts(name, buildCreateOpts(cfg, branch, repoNames, repoMap))
		if err != nil {
			failCreate(err, replacedName)
		}
		result.Replaced = replacedName

		wsPath := filepath.Join(cfg.WorkspaceDir, name)
		firePostCreateHook(name, wsPath, branch)

		machine.Emit(result,
			machine.NextAction("Inspect repo state", "gw status "+name+" --format json"),
			machine.NextAction("Run configured processes", "gw run "+name+" --format json"),
		)
	},
}

// ---------------------------------------------------------------------------
// Repo selection
// ---------------------------------------------------------------------------

// resolveCreateRepos determines which repos the workspace will contain. The four
// sources are mutually exclusive and checked in precedence order: an explicit
// preset, every discovered repo, an explicit list, then interactive selection.
//
// It may add entries to repoMap, since an explicit list can name a clone URL.
func resolveCreateRepos(cfg *models.Config, repos []discover.Repo, repoMap map[string]string) []string {
	switch {
	case createPreset != "":
		return reposFromPreset(cfg)
	case createAll:
		return repoNamesList(repos)
	case createRepos != "":
		return reposFromFlag(cfg, repoMap)
	default:
		return reposInteractively(cfg, repos)
	}
}

func reposFromPreset(cfg *models.Config) []string {
	preset, ok := cfg.Presets[createPreset]
	if !ok {
		fail(machine.Errorf(machine.CodeUsage, "preset %s not found", createPreset).
			WithActions(machine.NextAction("List presets", "gw preset list --format json")))
	}
	return preset.Repos
}

// reposFromFlag reads --repos, cloning any entry that is a git URL. That lets a
// resolver (e.g. a PR-to-workspace plugin) pass a repo Grove has never seen.
func reposFromFlag(cfg *models.Config, repoMap map[string]string) []string {
	repoNames := parseRepoList(createRepos)
	for i, name := range repoNames {
		if gitops.IsGitURL(name) {
			repoNames[i] = cloneRepo(cfg, repoMap, name)
		}
	}
	return repoNames
}

// cloneRepo clones a URL into the first configured repo dir and registers it in
// repoMap, returning the local repo name.
func cloneRepo(cfg *models.Config, repoMap map[string]string, url string) string {
	if len(cfg.RepoDirs) == 0 {
		fail(machine.Errorf(machine.CodeNotInitialized, "no repo_dirs configured — cannot clone %s", url).
			WithActions(machine.NextAction("Add a repo directory", "gw add-dir <path>")))
	}

	console.Infof("Cloning %s ...", url)
	clonedPath, repoName, err := gitops.Clone(url, cfg.RepoDirs[0])
	if err != nil {
		fail(machine.Wrap(machine.CodeTransient, err, "cloning %s: %s", url, err).
			WithFix("Check network access and repository permissions, then retry"))
	}
	// A different local clone under the same name would make the workspace
	// ambiguous about which checkout it is using.
	if existing, ok := repoMap[repoName]; ok && existing != clonedPath {
		fail(machine.Errorf(machine.CodeBranchConflict,
			"repo name conflict: %s already exists locally at %s", repoName, existing))
	}

	repoMap[repoName] = clonedPath
	console.Successf("Cloned %s into %s", repoName, clonedPath)
	return repoName
}

// reposInteractively asks the user. With presets configured it offers those first
// (with a manual escape hatch); otherwise it goes straight to the repo list and
// offers to save the selection as a preset.
//
// In machine mode the pickers refuse to run and return a USAGE error, so no branch
// here can block on input.
func reposInteractively(cfg *models.Config, repos []discover.Repo) []string {
	repoChoices := repoNamesList(repos)

	if len(cfg.Presets) == 0 {
		selected := pickRepos(repoChoices)
		offerPresetSave(cfg, selected, len(repos))
		return selected
	}

	if fromPreset, ok := pickPreset(cfg); ok {
		return fromPreset
	}
	return pickRepos(repoChoices)
}

func pickRepos(repoChoices []string) []string {
	selected, err := prompter.PickMany("Select repos for workspace:", repoChoices)
	if err != nil {
		exitOnPickerErr(err)
	}
	return selected
}

// pickPreset offers the configured presets. The second return is false when the
// user chose to pick repos manually instead.
func pickPreset(cfg *models.Config) ([]string, bool) {
	names := make([]string, 0, len(cfg.Presets))
	choices := make([]string, 0, len(cfg.Presets)+1)
	for name, p := range cfg.Presets {
		names = append(names, name)
		choices = append(choices, name+"  ("+strings.Join(p.Repos, ", ")+")")
	}
	choices = append(choices, pickManuallyChoice)

	choice, err := prompter.PickOne("Select repos from:", choices)
	if err != nil {
		exitOnPickerErr(err)
	}
	if choice == pickManuallyChoice {
		return nil, false
	}

	for i, display := range choices {
		if display == choice && i < len(names) {
			return cfg.Presets[names[i]].Repos, true
		}
	}
	return nil, false
}

// offerPresetSave suggests saving a partial selection as a preset, so the next
// create can skip the picker. Only worth asking for a real subset, and only when
// there is a human to ask.
func offerPresetSave(cfg *models.Config, selected []string, totalRepos int) {
	if !prompter.Interactive() || len(selected) >= totalRepos {
		return
	}
	if !prompter.Confirm("Save this selection as a preset?", false) {
		return
	}

	presetName := prompter.Prompt("Preset name", "")
	if presetName == "" {
		return
	}

	if cfg.Presets == nil {
		cfg.Presets = make(map[string]models.Preset)
	}
	cfg.Presets[presetName] = models.Preset{Repos: selected}
	if err := config.Save(cfg); err != nil {
		console.Warningf("Could not save preset: %s", err)
		return
	}
	console.Successf("Saved preset %q", presetName)
}

// requireKnownRepos rejects names that are not discoverable, listing what is
// available so the caller can correct itself in one round trip.
func requireKnownRepos(repoNames []string, repoMap map[string]string, repos []discover.Repo) {
	for _, name := range repoNames {
		if _, ok := repoMap[name]; !ok {
			fail(workspace.ErrRepoNotFound(name).
				WithDetails(map[string]any{"available": repoNamesList(repos)}))
		}
	}
}

// ---------------------------------------------------------------------------
// Name, branch, and provenance
// ---------------------------------------------------------------------------

// resolveCreateBranch returns the branch to create, prompting when a human omitted
// --branch. name seeds the prompt's default, which is why the branch is resolved
// after the name argument is read.
func resolveCreateBranch(name string) string {
	if createBranch != "" {
		return createBranch
	}

	requireArgs("--branch", "gw create "+name+" -b feat/x --format json")

	branch := ""
	if prompter.Interactive() {
		branch = prompter.Prompt("Branch name", name)
	}
	if branch == "" {
		fail(machine.Errorf(machine.CodeUsage, "branch is required").
			WithFix("Pass --branch / -b"))
	}
	return branch
}

// buildCreateOpts assembles the service request. Source provenance is opaque to
// Grove core — recorded and passed to hooks, never interpreted.
func buildCreateOpts(cfg *models.Config, branch string, repoNames []string, repoMap map[string]string) workspace.CreateOpts {
	opts := workspace.CreateOpts{
		Branch:  branch,
		Repos:   repoNames,
		RepoMap: repoMap,
		Cfg:     cfg,
		Source:  createSource(),
	}
	// --track checks out an existing remote branch (e.g. a PR head) instead of
	// creating one, falling back to create-mode when it is missing.
	if createTrack {
		opts.BranchMode = workspace.BranchModeTrack
	}
	return opts
}

func createSource() *models.WorkspaceSource {
	if createSourceURL == "" && createSourceProvide == "" {
		return nil
	}
	return &models.WorkspaceSource{
		Provider: createSourceProvide,
		URL:      createSourceURL,
		Ref:      createSourceRef,
		Title:    createSourceTitle,
	}
}

// ---------------------------------------------------------------------------
// --replace
// ---------------------------------------------------------------------------

// replaceCurrentWorkspace handles --replace: delete the workspace containing the
// cwd before creating the new one. It returns the deleted workspace's name, or ""
// when --replace was not requested.
//
// Every check happens before the deletion, because once the old workspace is gone
// a later failure leaves the user with neither workspace.
func replaceCurrentWorkspace(name string) string {
	if !createReplace {
		return ""
	}

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
	// destructive intent be explicit rather than inferred from a skipped prompt.
	if !createForce {
		requireArgs("--force (with --replace)", "gw create "+name+" -b <branch> --replace --force --format json")
		if !prompter.Confirm("Delete workspace "+currentWs.Name+" and replace with "+name+"?", false) {
			os.Exit(0)
		}
	}

	console.Infof("Replacing workspace: deleting %s", currentWs.Name)
	firePreDeleteHook(*currentWs)

	if _, err := workspace.NewService().Delete(currentWs.Name); err != nil {
		fail(machine.Wrap(machine.CodeFor(err), err, "failed to delete current workspace: %s", err))
	}
	return currentWs.Name
}

// failCreate reports a creation failure, making it explicit when --replace has
// already destroyed the previous workspace — that is not a no-op failure, and the
// caller must not assume nothing changed.
func failCreate(err error, replacedName string) {
	if replacedName == "" {
		fail(err)
	}
	fail(machine.Wrap(machine.CodeFor(err), err,
		"failed to create new workspace (old workspace %s was already deleted): %s", replacedName, err).
		WithDetails(map[string]any{"deleted_workspace": replacedName}))
}

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

func firePreDeleteHook(ws models.Workspace) {
	vars := lifecycle.Vars{Name: ws.Name, Path: ws.Path, Branch: ws.Branch}
	if err := lifecycle.Run("pre_delete", vars); err != nil && !errors.Is(err, lifecycle.ErrNoHook) {
		if lifecycle.ShouldAbort(err) {
			fail(machine.Wrap(machine.CodeHookFailed, err, "%s", err))
		}
		console.Warning(err.Error())
	}
}

func firePostCreateHook(name, wsPath, branch string) {
	vars := lifecycle.Vars{Name: name, Path: wsPath, Branch: branch}
	if source := createSource(); source != nil {
		vars.SourceURL = source.URL
		vars.SourceRef = source.Ref
		vars.SourceTitle = source.Title
	}

	err := lifecycle.Run("post_create", vars)
	if err == nil || errors.Is(err, lifecycle.ErrNoHook) {
		return
	}
	if lifecycle.ShouldAbort(err) {
		// The workspace exists; the hook is what failed. Report the workspace in
		// details so the caller does not retry create.
		fail(machine.Wrap(machine.CodeHookFailed, err, "%s", err).
			WithDetails(map[string]any{"workspace": name, "path": wsPath}).
			WithFix("Fix the post_create hook, or re-run with --no-hooks"))
	}
	console.Warning(err.Error())
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
