package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
)

func TestCreateSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	err := env.createWorkspace("test-ws", "feat/test", []string{"api", "web"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Workspace saved to state
	ws, _ := env.svc.State.GetWorkspace("test-ws")
	if ws == nil {
		t.Fatal("workspace not in state")
	}
	if len(ws.Repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(ws.Repos))
	}

	// Worktree directories exist
	for _, r := range ws.Repos {
		if _, err := os.Stat(r.WorktreePath); os.IsNotExist(err) {
			t.Errorf("worktree dir missing: %s", r.WorktreePath)
		}
	}

	// Branch correct in worktrees
	branch := env.run(filepath.Join(env.wsDir, "test-ws", "api"), "git", "branch", "--show-current")
	if branch != "feat/test" {
		t.Errorf("expected branch feat/test, got %s", branch)
	}
}

func TestCreateWithPreparationUsesExactCommitOutsideLockAndSkipsSetup(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	baseSHA := env.run(repo, "git", "rev-parse", "HEAD")
	setupMarker := filepath.Join(env.dir, "legacy-setup-ran")
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte("setup = \"touch "+setupMarker+"\"\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "later.txt"), []byte("later"), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "later")

	var preparedPath string
	err := env.svc.CreateWithPreparation("prepared", PreparationOpts{
		CreateOpts:  CreateOpts{Branch: "feat/prepared", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg},
		BaseCommits: map[string]string{"api": baseSHA},
	}, func(ws models.Workspace) error {
		if err := env.svc.State.WithLock(func() error { return nil }); err != nil {
			return fmt.Errorf("acquiring state lock from preparation: %w", err)
		}
		preparedPath = ws.Repos[0].WorktreePath
		if got := env.run(preparedPath, "git", "rev-parse", "HEAD"); got != baseSHA {
			return fmt.Errorf("worktree HEAD = %s, want %s", got, baseSHA)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if preparedPath == "" {
		t.Fatal("preparation callback did not run")
	}
	if _, err := os.Stat(setupMarker); !os.IsNotExist(err) {
		t.Fatalf("legacy setup hook ran during Recipe creation: %v", err)
	}
}

func TestCreateWithPreparationFailurePreservesPreexistingBranch(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	baseSHA := env.run(repo, "git", "rev-parse", "HEAD")
	env.run(repo, "git", "branch", "feat/existing", baseSHA)
	boom := errors.New("prepare failed")

	err := env.svc.CreateWithPreparation("prepared", PreparationOpts{
		CreateOpts:  CreateOpts{Branch: "feat/existing", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg},
		BaseCommits: map[string]string{"api": baseSHA},
	}, func(models.Workspace) error { return boom })
	var preparationErr *PreparationError
	if !errors.As(err, &preparationErr) || !errors.Is(err, boom) {
		t.Fatalf("error = %v, want PreparationError wrapping callback failure", err)
	}
	if !gitops.BranchExists(repo, "feat/existing") {
		t.Fatal("pre-existing branch was deleted")
	}
	if ws, _ := env.svc.State.GetWorkspace("prepared"); ws != nil {
		t.Fatalf("failed preparation remained in state: %+v", ws)
	}
	if _, err := os.Stat(filepath.Join(env.wsDir, "prepared")); !os.IsNotExist(err) {
		t.Fatalf("workspace directory remained after rollback: %v", err)
	}
}

func TestCreateWithPreparationFailureRemovesOwnedBranch(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	baseSHA := env.run(repo, "git", "rev-parse", "HEAD")

	_ = env.svc.CreateWithPreparation("prepared", PreparationOpts{
		CreateOpts:  CreateOpts{Branch: "feat/owned", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg},
		BaseCommits: map[string]string{"api": baseSHA},
	}, func(models.Workspace) error { return errors.New("prepare failed") })
	if gitops.BranchExists(repo, "feat/owned") {
		t.Fatal("branch created by failed preparation was preserved")
	}
}

func TestCreateWithPreparationRejectsSuccessfulIdentityChange(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	baseSHA := env.run(repo, "git", "rev-parse", "HEAD")

	err := env.svc.CreateWithPreparation("prepared", PreparationOpts{
		CreateOpts:  CreateOpts{Branch: "feat/prepared", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg},
		BaseCommits: map[string]string{"api": baseSHA},
	}, func(ws models.Workspace) error {
		env.run(ws.Repos[0].WorktreePath, "git", "checkout", "--detach")
		return nil
	})
	var preparationErr *PreparationError
	if !errors.As(err, &preparationErr) || preparationErr.CleanupErr == nil {
		t.Fatalf("error = %v, want identity verification failure", err)
	}
	if ws, _ := env.svc.State.GetWorkspace("prepared"); ws == nil {
		t.Fatal("changed worktree state should remain recoverable")
	}
}

func TestCreateWithPreparationRefusesRollbackAfterWorktreeIdentityChanges(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	baseSHA := env.run(repo, "git", "rev-parse", "HEAD")

	err := env.svc.CreateWithPreparation("prepared", PreparationOpts{
		CreateOpts:  CreateOpts{Branch: "feat/prepared", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg},
		BaseCommits: map[string]string{"api": baseSHA},
	}, func(ws models.Workspace) error {
		env.run(ws.Repos[0].WorktreePath, "git", "checkout", "--detach")
		return errors.New("prepare failed")
	})
	var preparationErr *PreparationError
	if !errors.As(err, &preparationErr) || preparationErr.CleanupErr == nil {
		t.Fatalf("error = %v, want identity cleanup refusal", err)
	}
	if ws, _ := env.svc.State.GetWorkspace("prepared"); ws == nil {
		t.Fatal("workspace state was removed after worktree identity changed")
	}
	if _, err := os.Stat(filepath.Join(env.wsDir, "prepared", "api")); err != nil {
		t.Fatalf("changed worktree was removed: %v", err)
	}
}

func TestCreateWithPreparationRefusesRollbackAfterStateChanges(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	baseSHA := env.run(repo, "git", "rev-parse", "HEAD")

	err := env.svc.CreateWithPreparation("prepared", PreparationOpts{
		CreateOpts:  CreateOpts{Branch: "feat/prepared", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg},
		BaseCommits: map[string]string{"api": baseSHA},
	}, func(ws models.Workspace) error {
		ws.Branch = "concurrently-changed"
		if err := env.svc.State.UpdateWorkspace(ws); err != nil {
			return err
		}
		return errors.New("prepare failed")
	})
	var preparationErr *PreparationError
	if !errors.As(err, &preparationErr) || preparationErr.CleanupErr == nil {
		t.Fatalf("error = %v, want cleanup refusal", err)
	}
	if ws, _ := env.svc.State.GetWorkspace("prepared"); ws == nil || ws.Branch != "concurrently-changed" {
		t.Fatalf("changed workspace state was removed: %+v", ws)
	}
	if _, err := os.Stat(filepath.Join(env.wsDir, "prepared", "api")); err != nil {
		t.Fatalf("worktree was removed despite changed state: %v", err)
	}
}

func TestCreateDuplicateNameFails(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	env.createWorkspace("dupe", "feat/a", []string{"api"})

	err := env.createWorkspace("dupe", "feat/b", []string{"api"})
	if err == nil {
		t.Error("expected error for duplicate workspace name")
	}
}

func TestCreateDuplicateBranchFails(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	env.createWorkspace("ws1", "feat/shared", []string{"api"})

	// Second workspace with same branch on same repo should fail
	err := env.createWorkspace("ws2", "feat/shared", []string{"api"})
	if err == nil {
		t.Error("expected error for duplicate branch on same repo")
	}
}

func TestCreateRollbackOnFailure(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Try to create with a nonexistent repo — should rollback
	err := env.createWorkspace("rollback-ws", "feat/test", []string{"api", "nonexistent"})
	if err == nil {
		t.Error("expected error")
	}

	// Workspace dir should be cleaned up
	wsPath := filepath.Join(env.wsDir, "rollback-ws")
	if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
		t.Error("workspace dir should be removed on rollback")
	}

	// State should not contain the workspace
	ws, _ := env.svc.State.GetWorkspace("rollback-ws")
	if ws != nil {
		t.Error("workspace should not be in state after rollback")
	}
}

func TestCreateFailurePreservesPreexistingWorkspaceRoot(t *testing.T) {
	env := setupTestEnv(t)
	path := filepath.Join(env.wsDir, "preexisting-root")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("create pre-existing root: %v", err)
	}

	err := env.createWorkspace("preexisting-root", "feat/preexisting-root", []string{"missing"})
	if err == nil {
		t.Fatal("expected missing repo error")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pre-existing workspace root should remain: %v", err)
	}
}

func TestCreateRollbackRemovesBranchesCreatedByInvocation(t *testing.T) {
	env := setupTestEnv(t)
	api := env.createRepo("api")
	web := env.createRepo("web")

	conflictPath := filepath.Join(env.dir, "web-conflict")
	env.run(web, "git", "worktree", "add", "-q", "-b", "feat/create-rollback", conflictPath)

	err := env.createWorkspace("create-rollback", "feat/create-rollback", []string{"api", "web"})
	if err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("expected web conflict, got %v", err)
	}
	if gitops.BranchExists(api, "feat/create-rollback") {
		t.Error("rollback should remove the api branch created by this invocation")
	}
	if _, err := os.Stat(filepath.Join(env.wsDir, "create-rollback")); !os.IsNotExist(err) {
		t.Error("empty workspace root should be removed after rollback")
	}
}

func TestConcurrentCreatesPreserveBothStateEntries(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	svcA := *env.svc
	svcA.State = state.NewStore(env.groveDir)
	svcA.Stats = &stats.Tracker{StatsPath: filepath.Join(env.groveDir, "stats-a.json"), NowFn: time.Now}
	svcB := *env.svc
	svcB.State = state.NewStore(env.groveDir)
	svcB.Stats = &stats.Tracker{StatsPath: filepath.Join(env.groveDir, "stats-b.json"), NowFn: time.Now}

	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		errs <- svcA.CreateWithOpts("ws-a", CreateOpts{
			Branch: "feat/a", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
		})
	}()
	go func() {
		<-start
		errs <- svcB.CreateWithOpts("ws-b", CreateOpts{
			Branch: "feat/b", Repos: []string{"web"}, RepoMap: env.repoMap, Cfg: env.cfg,
		})
	}()
	close(start)

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent create: %v", err)
		}
	}

	workspaces, err := env.svc.State.Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("expected both workspaces in state, got %d", len(workspaces))
	}
}

func TestCreateAutoCreatesBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Branch doesn't exist yet — should be auto-created
	err := env.createWorkspace("auto-branch", "feat/new-branch", []string{"api"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Verify the branch exists in the worktree
	branch := env.run(filepath.Join(env.wsDir, "auto-branch", "api"), "git", "branch", "--show-current")
	if branch != "feat/new-branch" {
		t.Errorf("expected feat/new-branch, got %s", branch)
	}
}

func TestCreateTrackModeChecksOutExistingBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.pushRemoteBranch(env.repoMap["api"], "feat/pr-head", "pr-marker.txt")

	err := env.svc.CreateWithOpts("pr-ws", CreateOpts{
		Branch:          "feat/pr-head",
		Repos:           []string{"api"},
		RepoMap:         env.repoMap,
		Cfg:             env.cfg,
		BranchMode:      BranchModeTrack,
		TrackBranchRepo: "api",
	})
	if err != nil {
		t.Fatalf("create track: %v", err)
	}

	wt := filepath.Join(env.wsDir, "pr-ws", "api")
	// The PR head's marker file must be present (we checked out the existing branch).
	if _, err := os.Stat(filepath.Join(wt, "pr-marker.txt")); os.IsNotExist(err) {
		t.Error("expected PR head content (pr-marker.txt) in tracking worktree")
	}
	// The worktree branch must track origin/feat/pr-head.
	upstream := env.run(wt, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if upstream != "origin/feat/pr-head" {
		t.Errorf("expected upstream origin/feat/pr-head, got %q", upstream)
	}
}

func TestCreateTrackModeFallsBackWhenRemoteMissing(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")

	// Track mode requested but no such remote branch exists → fall back to
	// creating a new branch from base (no error).
	err := env.svc.CreateWithOpts("fallback-ws", CreateOpts{
		Branch:          "feat/ghost-pr",
		Repos:           []string{"api"},
		RepoMap:         env.repoMap,
		Cfg:             env.cfg,
		BranchMode:      BranchModeTrack,
		TrackBranchRepo: "api",
	})
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}

	wt := filepath.Join(env.wsDir, "fallback-ws", "api")
	branch := env.run(wt, "git", "branch", "--show-current")
	if branch != "feat/ghost-pr" {
		t.Errorf("expected new branch feat/ghost-pr, got %q", branch)
	}
}

func TestCreateTrackModeOnlyAppliesToDesignatedRepo(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createRepoWithRemote("web")
	env.pushRemoteBranch(env.repoMap["api"], "feat/shared", "api-pr.txt")

	// Both repos share the branch name, but only "api" is the track repo;
	// "web" should create a fresh branch from base (no api-pr.txt leakage).
	err := env.svc.CreateWithOpts("mixed-ws", CreateOpts{
		Branch:          "feat/shared",
		Repos:           []string{"api", "web"},
		RepoMap:         env.repoMap,
		Cfg:             env.cfg,
		BranchMode:      BranchModeTrack,
		TrackBranchRepo: "api",
	})
	if err != nil {
		t.Fatalf("create mixed: %v", err)
	}

	apiWT := filepath.Join(env.wsDir, "mixed-ws", "api")
	if _, err := os.Stat(filepath.Join(apiWT, "api-pr.txt")); os.IsNotExist(err) {
		t.Error("api worktree should have tracked the PR head (api-pr.txt missing)")
	}
	webWT := filepath.Join(env.wsDir, "mixed-ws", "web")
	if _, err := os.Stat(filepath.Join(webWT, "api-pr.txt")); err == nil {
		t.Error("web worktree should NOT contain api's PR content — create mode expected")
	}
}

func TestCreateWithOptsPersistsSource(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	src := &models.WorkspaceSource{
		Provider: "github",
		URL:      "https://github.com/acme/example-app/pull/1172",
		Ref:      "1172",
		Title:    "Example pull request",
	}
	err := env.svc.CreateWithOpts("src-ws", CreateOpts{
		Branch:  "feat/src",
		Repos:   []string{"api"},
		RepoMap: env.repoMap,
		Cfg:     env.cfg,
		Source:  src,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("src-ws")
	if ws == nil || ws.Source == nil {
		t.Fatal("expected persisted Source on workspace")
	}
	if *ws.Source != *src {
		t.Errorf("source mismatch: got %+v, want %+v", *ws.Source, *src)
	}
}

func TestCreateDoesNotWriteMCPConfig(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	if err := env.createWorkspace("plain-ws", "feat/plain", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	mcpPath := filepath.Join(env.wsDir, "plain-ws", ".mcp.json")
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Fatalf("Grove should not create .mcp.json, stat error: %v", err)
	}
}

func TestCreateCdFile(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	cdFile := filepath.Join(env.dir, "cd-target")
	os.Setenv("GROVE_CD_FILE", cdFile)
	defer os.Unsetenv("GROVE_CD_FILE")

	env.createWorkspace("cd-ws", "feat/cd", []string{"api"})

	data, err := os.ReadFile(cdFile)
	if err != nil {
		t.Fatalf("reading cd file: %v", err)
	}
	expected := filepath.Join(env.wsDir, "cd-ws")
	if string(data) != expected {
		t.Errorf("cd file: got %q, want %q", string(data), expected)
	}
}

func TestSetupHookRuns(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("hooked")

	// Write .grove.toml with setup hook
	toml := `setup = "touch .setup-ran"`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add grove config")

	env.createWorkspace("hook-ws", "feat/hook", []string{"hooked"})

	// Check marker file in worktree (not source repo)
	marker := filepath.Join(env.wsDir, "hook-ws", "hooked", ".setup-ran")
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Error("setup hook did not run in worktree")
	}
}

func TestSetupHookMultipleCommands(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("multi")

	toml := `setup = ["touch .step1", "touch .step2"]`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add grove config")

	env.createWorkspace("multi-ws", "feat/multi", []string{"multi"})

	wt := filepath.Join(env.wsDir, "multi-ws", "multi")
	for _, f := range []string{".step1", ".step2"} {
		if _, err := os.Stat(filepath.Join(wt, f)); os.IsNotExist(err) {
			t.Errorf("setup hook step %s did not run", f)
		}
	}
}

func TestNoSetupHookNoCrash(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("plain")

	err := env.createWorkspace("plain-ws", "feat/plain", []string{"plain"})
	if err != nil {
		t.Fatalf("should not fail without setup hook: %v", err)
	}
}

func TestCreateMultiRepoAllProcessed(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createRepo("worker")

	err := env.createWorkspace("multi-ws", "feat/multi", []string{"api", "web", "worker"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("multi-ws")
	if len(ws.Repos) != 3 {
		t.Errorf("expected 3 repos, got %d", len(ws.Repos))
	}

	// All worktrees exist
	for _, name := range []string{"api", "web", "worker"} {
		wt := filepath.Join(env.wsDir, "multi-ws", name)
		if _, err := os.Stat(wt); os.IsNotExist(err) {
			t.Errorf("worktree %s missing", name)
		}
	}
}

func TestSetupHookUsesRunCmd(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")

	toml := `setup = "echo injected-setup"`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add setup")

	var setupCalls []string
	origRunCmd := env.svc.RunCmd
	env.svc.RunCmd = func(dir, cmd string) error {
		setupCalls = append(setupCalls, cmd)
		return nil
	}
	defer func() { env.svc.RunCmd = origRunCmd }()

	env.createWorkspace("inject-ws", "feat/inject", []string{"api"})

	found := false
	for _, c := range setupCalls {
		if strings.Contains(c, "injected-setup") {
			found = true
		}
	}
	if !found {
		t.Errorf("setup hook should use RunCmd; calls: %v", setupCalls)
	}
}

func TestCreateShowsProgressOnStderr(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	env.createWorkspace("progress-ws", "feat/progress", []string{"api", "web"})

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stderr = oldStderr

	output := buf.String()

	// Should show fetch progress then per-repo progress
	if !strings.Contains(output, "fetching 2 repos") {
		t.Errorf("expected 'fetching 2 repos' in output, got: %q", output)
	}
	if !strings.Contains(output, "[1/2]") {
		t.Errorf("expected [1/2] progress, got: %q", output)
	}
	if !strings.Contains(output, "[2/2]") {
		t.Errorf("expected [2/2] progress, got: %q", output)
	}
}
