package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/models"
)

func TestContextInsideWorkspace(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("ctx-ws", "feat/ctx", []string{"api", "web"}, env.repoMap, env.cfg)

	wsPath := filepath.Join(env.wsDir, "ctx-ws")
	ctx, err := env.svc.Context(wsPath, "test", env.cfg)
	if err != nil {
		t.Fatalf("context: %v", err)
	}

	if !ctx.Initialized {
		t.Error("initialized should be true with a config")
	}
	if ctx.Workspace == nil {
		t.Fatal("expected to resolve the containing workspace")
	}
	if ctx.Workspace.Name != "ctx-ws" || ctx.Workspace.Branch != "feat/ctx" {
		t.Errorf("workspace = %s/%s", ctx.Workspace.Name, ctx.Workspace.Branch)
	}
	if len(ctx.Workspace.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(ctx.Workspace.Repos))
	}
	if ctx.WorkspaceCount != 1 || len(ctx.Workspaces) != 1 {
		t.Errorf("workspace inventory = %d/%v", ctx.WorkspaceCount, ctx.Workspaces)
	}
	for _, r := range ctx.Workspace.Repos {
		if r.Dirty {
			t.Errorf("%s should be clean", r.Repo)
		}
		if r.Path == "" || r.SourceRepo == "" {
			t.Errorf("%s missing paths: %+v", r.Repo, r)
		}
		if r.Status == "" {
			t.Errorf("%s missing git status", r.Repo)
		}
	}
}

// A repo's worktree is inside the workspace, so running from there must resolve
// the same workspace — agents run commands from repo directories.
func TestContextFromRepoSubdirectory(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("sub-ws", "feat/sub", []string{"api"}, env.repoMap, env.cfg)

	deep := filepath.Join(env.wsDir, "sub-ws", "api")
	ctx, err := env.svc.Context(deep, "test", env.cfg)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if ctx.Workspace == nil || ctx.Workspace.Name != "sub-ws" {
		t.Fatalf("expected sub-ws, got %+v", ctx.Workspace)
	}
}

func TestContextReportsDirtyRepo(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("dirty-ctx", "feat/dirty", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "dirty-ctx", "api")
	os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("wip"), 0o644)

	ctx, err := env.svc.Context(wt, "test", env.cfg)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if !ctx.Workspace.Repos[0].Dirty {
		t.Error("expected the repo to be reported dirty")
	}
}

// Outside any workspace, workspace must be null rather than a guess — that null
// is what tells an agent it has to name a workspace explicitly.
func TestContextOutsideWorkspaceIsNull(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("some-ws", "feat/some", []string{"api"}, env.repoMap, env.cfg)

	ctx, err := env.svc.Context(env.reposDir, "test", env.cfg)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if ctx.Workspace != nil {
		t.Errorf("expected no workspace, got %+v", ctx.Workspace)
	}
	if ctx.WorkspaceCount != 1 {
		t.Errorf("workspace_count = %d, want 1", ctx.WorkspaceCount)
	}
}

// A sibling directory sharing a name prefix must not be mistaken for being
// inside the workspace.
func TestContextRejectsSiblingPathPrefix(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("feat", "feat/one", []string{"api"}, env.repoMap, env.cfg)

	sibling := filepath.Join(env.wsDir, "feat-other")
	os.MkdirAll(sibling, 0o755)

	ctx, err := env.svc.Context(sibling, "test", env.cfg)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if ctx.Workspace != nil {
		t.Errorf("feat-other is not inside feat, got %+v", ctx.Workspace)
	}
}

// No config is a reportable state, not a failure: it is how an agent discovers
// that Grove needs `gw init`.
func TestContextWithoutConfig(t *testing.T) {
	env := setupTestEnv(t)

	ctx, err := env.svc.Context(env.dir, "test", nil)
	if err != nil {
		t.Fatalf("context should not fail without config: %v", err)
	}
	if ctx.Initialized {
		t.Error("initialized should be false without a config")
	}
	if ctx.RepoDirs == nil || ctx.Presets == nil || ctx.Workspaces == nil {
		t.Error("list fields must be empty arrays, never null")
	}
}

func TestContextListsPresetsSorted(t *testing.T) {
	env := setupTestEnv(t)
	env.cfg.Presets = map[string]models.Preset{
		"zeta":    {Repos: []string{"api"}},
		"alpha":   {Repos: []string{"web"}},
		"backend": {Repos: []string{"api", "web"}},
	}

	ctx, err := env.svc.Context(env.dir, "test", env.cfg)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	want := []string{"alpha", "backend", "zeta"}
	for i, name := range want {
		if ctx.Presets[i] != name {
			t.Fatalf("presets = %v, want %v", ctx.Presets, want)
		}
	}
}

// Context and status must agree; they are two views of one collection path.
func TestContextGitStateMatchesStatus(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("agree-ws", "feat/agree", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "agree-ws", "api")
	os.WriteFile(filepath.Join(wt, "change.txt"), []byte("x"), 0o644)

	ctx, err := env.svc.Context(wt, "test", env.cfg)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	report, err := env.svc.StatusReport("agree-ws", StatusOptions{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if ctx.Workspace.Repos[0].Status != report.Repos[0].Status {
		t.Errorf("context status %q != status command %q",
			ctx.Workspace.Repos[0].Status, report.Repos[0].Status)
	}
}
