package cmd

import (
	"testing"

	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
)

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
