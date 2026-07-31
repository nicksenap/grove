package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
)

// withConfigPath points config.Save at a temp file so a test can exercise a flow
// that persists configuration without touching the real one.
func withConfigPath(t *testing.T, path string) func() {
	t.Helper()
	original := config.ConfigPath
	config.ConfigPath = path
	return func() { config.ConfigPath = original }
}

func TestDeriveName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"feat/login", "feat-login"},
		{"feat/my feature", "feat-my-feature"},
		{"main", "main"},
		{"/leading", "leading"},
		{"trailing/", "trailing"},
		{"/both/", "both"},
		{"a/b/c", "a-b-c"},
		{"  spaced  ", "spaced"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := deriveName(tt.in); got != tt.want {
				t.Errorf("deriveName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRepoNamesList(t *testing.T) {
	repos := []discover.Repo{
		{Name: "api"},
		{Name: "web"},
		{Name: "worker"},
	}
	got := repoNamesList(repos)
	want := []string{"api", "web", "worker"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRepoNamesList_Empty(t *testing.T) {
	got := repoNamesList(nil)
	if got == nil {
		t.Error("expected non-nil empty slice (callers strings.Join), got nil")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// Repo selection precedence
// ---------------------------------------------------------------------------

// resetCreateFlags clears the package-level flag state between cases, since Cobra
// binds these globally.
func resetCreateFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		createPreset, createRepos, createBranch = "", "", ""
		createAll, createTrack, createReplace, createForce = false, false, false, false
		createSourceURL, createSourceProvide, createSourceRef, createSourceTitle = "", "", "", ""
	})
	createPreset, createRepos, createBranch = "", "", ""
	createAll, createTrack, createReplace, createForce = false, false, false, false
	createSourceURL, createSourceProvide, createSourceRef, createSourceTitle = "", "", "", ""
}

func TestResolveCreateReposPrecedence(t *testing.T) {
	repos := []discover.Repo{{Name: "api"}, {Name: "web"}, {Name: "worker"}}
	repoMap := map[string]string{"api": "/r/api", "web": "/r/web", "worker": "/r/worker"}
	cfg := &models.Config{
		RepoDirs: []string{"/r"},
		Presets:  map[string]models.Preset{"backend": {Repos: []string{"api", "worker"}}},
	}

	t.Run("preset wins over --all and --repos", func(t *testing.T) {
		resetCreateFlags(t)
		createPreset, createAll, createRepos = "backend", true, "web"
		got := resolveCreateRepos(cfg, repos, repoMap)
		if len(got) != 2 || got[0] != "api" || got[1] != "worker" {
			t.Errorf("got %v, want [api worker]", got)
		}
	})

	t.Run("--all wins over --repos", func(t *testing.T) {
		resetCreateFlags(t)
		createAll, createRepos = true, "web"
		got := resolveCreateRepos(cfg, repos, repoMap)
		if len(got) != 3 {
			t.Errorf("got %v, want every discovered repo", got)
		}
	})

	t.Run("--repos list", func(t *testing.T) {
		resetCreateFlags(t)
		createRepos = "web, worker"
		got := resolveCreateRepos(cfg, repos, repoMap)
		if len(got) != 2 || got[0] != "web" || got[1] != "worker" {
			t.Errorf("got %v, want [web worker]", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Provenance
// ---------------------------------------------------------------------------

// Source is opaque to core, but its presence rule matters: a workspace with no
// source flags must record nil rather than an empty struct, or every workspace
// would look like it came from somewhere.
func TestCreateSource(t *testing.T) {
	t.Run("absent without flags", func(t *testing.T) {
		resetCreateFlags(t)
		if got := createSource(); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("url alone is enough", func(t *testing.T) {
		resetCreateFlags(t)
		createSourceURL = "https://github.com/org/repo/pull/42"
		got := createSource()
		if got == nil || got.URL != createSourceURL {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("provider alone is enough", func(t *testing.T) {
		resetCreateFlags(t)
		createSourceProvide = "notion"
		if got := createSource(); got == nil || got.Provider != "notion" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("all fields recorded", func(t *testing.T) {
		resetCreateFlags(t)
		createSourceURL, createSourceProvide = "https://x/pull/1", "github"
		createSourceRef, createSourceTitle = "1", "Add login"
		got := createSource()
		if got.Ref != "1" || got.Title != "Add login" || got.Provider != "github" {
			t.Errorf("got %+v", got)
		}
	})
}

func TestBuildCreateOpts(t *testing.T) {
	cfg := &models.Config{WorkspaceDir: "/ws"}
	repoMap := map[string]string{"api": "/r/api"}

	t.Run("defaults to creating branches", func(t *testing.T) {
		resetCreateFlags(t)
		opts := buildCreateOpts(cfg, "feat/x", []string{"api"}, repoMap)
		if opts.BranchMode != workspace.BranchModeCreate {
			t.Errorf("BranchMode = %v, want create", opts.BranchMode)
		}
		if opts.Branch != "feat/x" || opts.Cfg != cfg || opts.Source != nil {
			t.Errorf("opts = %+v", opts)
		}
	})

	t.Run("--track switches to tracking mode", func(t *testing.T) {
		resetCreateFlags(t)
		createTrack = true
		if opts := buildCreateOpts(cfg, "feat/x", []string{"api"}, repoMap); opts.BranchMode != workspace.BranchModeTrack {
			t.Errorf("BranchMode = %v, want track", opts.BranchMode)
		}
	})
}

// ---------------------------------------------------------------------------
// Branch resolution
// ---------------------------------------------------------------------------

// An explicit --branch must be used verbatim, without consulting the terminal.
func TestResolveCreateBranchUsesFlag(t *testing.T) {
	resetCreateFlags(t)
	createBranch = "feat/explicit"
	if got := resolveCreateBranch("ignored-name"); got != "feat/explicit" {
		t.Errorf("got %q, want feat/explicit", got)
	}
}

// --replace is inert unless requested; it must not touch state or the cwd.
func TestReplaceCurrentWorkspaceNoopWithoutFlag(t *testing.T) {
	resetCreateFlags(t)
	if got := replaceCurrentWorkspace("anything"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// Interactive repo selection
// ---------------------------------------------------------------------------
//
// These paths were untestable before the Prompter seam: they need a terminal, so a
// non-interactive suite could only leave them uncovered.

func TestReposInteractivelyPicksAPreset(t *testing.T) {
	resetCreateFlags(t)
	p := newScriptedPrompter(t)
	p.picks["Select repos from"] = "backend  (api, worker)"
	withPrompter(t, p)

	cfg := &models.Config{Presets: map[string]models.Preset{
		"backend": {Repos: []string{"api", "worker"}},
	}}
	repos := []discover.Repo{{Name: "api"}, {Name: "web"}, {Name: "worker"}}

	got := reposInteractively(cfg, repos)
	if len(got) != 2 || got[0] != "api" || got[1] != "worker" {
		t.Errorf("got %v, want the preset's repos [api worker]", got)
	}
	if p.wasAsked("Select repos for workspace") {
		t.Errorf("choosing a preset should not also ask for individual repos: %s", p.askedList())
	}
}

// The escape hatch has to actually escape: choosing it must fall through to the
// repo list rather than returning an empty selection.
func TestReposInteractivelyFallsThroughToManualSelection(t *testing.T) {
	resetCreateFlags(t)
	p := newScriptedPrompter(t)
	p.picks["Select repos from"] = pickManuallyChoice
	p.multi["Select repos for workspace"] = []string{"web"}
	withPrompter(t, p)

	cfg := &models.Config{Presets: map[string]models.Preset{
		"backend": {Repos: []string{"api", "worker"}},
	}}
	repos := []discover.Repo{{Name: "api"}, {Name: "web"}, {Name: "worker"}}

	got := reposInteractively(cfg, repos)
	if len(got) != 1 || got[0] != "web" {
		t.Errorf("got %v, want the manual selection [web]", got)
	}
}

// With no presets configured there is nothing to offer, so the preset menu must be
// skipped entirely rather than shown empty.
func TestReposInteractivelySkipsPresetMenuWhenNoneExist(t *testing.T) {
	resetCreateFlags(t)
	p := newScriptedPrompter(t)
	p.multi["Select repos for workspace"] = []string{"api", "web"}
	p.confirms["Save this selection as a preset"] = false
	withPrompter(t, p)

	cfg := &models.Config{}
	repos := []discover.Repo{{Name: "api"}, {Name: "web"}, {Name: "worker"}}

	got := reposInteractively(cfg, repos)
	if len(got) != 2 {
		t.Fatalf("got %v, want two repos", got)
	}
	if p.wasAsked("Select repos from") {
		t.Errorf("no presets exist, so no preset menu should appear: %s", p.askedList())
	}
}

// ---------------------------------------------------------------------------
// Offering to save a preset
// ---------------------------------------------------------------------------

func TestOfferPresetSaveWritesTheConfig(t *testing.T) {
	resetCreateFlags(t)
	dir := t.TempDir()
	restore := withConfigPath(t, filepath.Join(dir, "config.toml"))
	defer restore()

	p := newScriptedPrompter(t)
	p.confirms["Save this selection as a preset"] = true
	p.inputs["Preset name"] = "backend"
	withPrompter(t, p)

	cfg := &models.Config{RepoDirs: []string{dir}, WorkspaceDir: dir}
	offerPresetSave(cfg, []string{"api", "worker"}, 3)

	preset, ok := cfg.Presets["backend"]
	if !ok {
		t.Fatalf("preset not saved: %+v", cfg.Presets)
	}
	if len(preset.Repos) != 2 || preset.Repos[0] != "api" {
		t.Errorf("preset repos = %v, want [api worker]", preset.Repos)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Errorf("config should have been written: %v", err)
	}
}

func TestOfferPresetSaveDeclined(t *testing.T) {
	resetCreateFlags(t)
	p := newScriptedPrompter(t)
	p.confirms["Save this selection as a preset"] = false
	withPrompter(t, p)

	cfg := &models.Config{}
	offerPresetSave(cfg, []string{"api"}, 3)
	if len(cfg.Presets) != 0 {
		t.Errorf("declining must not save anything, got %+v", cfg.Presets)
	}
}

// An empty name is a change of mind, not a preset called "".
func TestOfferPresetSaveEmptyName(t *testing.T) {
	resetCreateFlags(t)
	p := newScriptedPrompter(t)
	p.confirms["Save this selection as a preset"] = true
	p.inputs["Preset name"] = ""
	withPrompter(t, p)

	cfg := &models.Config{}
	offerPresetSave(cfg, []string{"api"}, 3)
	if len(cfg.Presets) != 0 {
		t.Errorf("an empty name must not create a preset, got %+v", cfg.Presets)
	}
}

// Saving the full set as a preset is pointless, and asking is noise.
func TestOfferPresetSaveNotOfferedForEverything(t *testing.T) {
	resetCreateFlags(t)
	p := newScriptedPrompter(t)
	withPrompter(t, p)

	cfg := &models.Config{}
	offerPresetSave(cfg, []string{"api", "web", "worker"}, 3)
	if p.wasAsked("Save this selection") {
		t.Errorf("selecting every repo should not prompt to save a preset: %s", p.askedList())
	}
}

// No human, no question — and no config write.
func TestOfferPresetSaveSkippedWhenNotInteractive(t *testing.T) {
	resetCreateFlags(t)
	p := newScriptedPrompter(t)
	p.interactive = false
	withPrompter(t, p)

	cfg := &models.Config{}
	offerPresetSave(cfg, []string{"api"}, 3)
	if len(p.asked) != 0 {
		t.Errorf("nothing should be asked without a terminal: %s", p.askedList())
	}
}

// ---------------------------------------------------------------------------
// Branch prompting
// ---------------------------------------------------------------------------

// The branch prompt defaults to the workspace name, which is why the name argument
// is read before the branch is resolved.
func TestResolveCreateBranchPromptsWithNameDefault(t *testing.T) {
	resetCreateFlags(t)
	p := newScriptedPrompter(t)
	p.inputs["Branch name"] = "feat/from-prompt"
	withPrompter(t, p)

	if got := resolveCreateBranch("my-workspace"); got != "feat/from-prompt" {
		t.Errorf("got %q, want the prompted branch", got)
	}
	if !p.wasAsked("Branch name") {
		t.Errorf("expected a branch prompt: %s", p.askedList())
	}
}

func TestResolveCreateBranchSkipsPromptWhenFlagGiven(t *testing.T) {
	resetCreateFlags(t)
	createBranch = "feat/flag"
	p := newScriptedPrompter(t)
	withPrompter(t, p)

	if got := resolveCreateBranch("name"); got != "feat/flag" {
		t.Errorf("got %q, want feat/flag", got)
	}
	if len(p.asked) != 0 {
		t.Errorf("an explicit --branch must not prompt: %s", p.askedList())
	}
}
