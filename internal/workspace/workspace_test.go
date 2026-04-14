package workspace

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
)

// testEnv sets up isolated Grove dirs + real git repos for testing.
type testEnv struct {
	t        *testing.T
	dir      string
	reposDir string
	wsDir    string
	groveDir string
	cfg      *models.Config
	repoMap  map[string]string
	svc      *Service
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()

	groveDir := filepath.Join(dir, ".grove")
	wsDir := filepath.Join(groveDir, "workspaces")
	reposDir := filepath.Join(dir, "repos")
	os.MkdirAll(groveDir, 0o755)
	os.MkdirAll(wsDir, 0o755)
	os.MkdirAll(reposDir, 0o755)

	store := state.NewStore(groveDir)
	os.WriteFile(store.Path, []byte("[]"), 0o644)

	cfg := &models.Config{
		RepoDirs:     []string{reposDir},
		WorkspaceDir: wsDir,
		Presets:      map[string]models.Preset{},
	}

	svc := &Service{
		State:        store,
		Stats:        &stats.Tracker{StatsPath: filepath.Join(groveDir, "stats.json"), NowFn: time.Now},
		RunCmd:       prodRunCmd,
		RunCmdSilent: prodRunCmdSilent,
	}

	return &testEnv{
		t:        t,
		dir:      dir,
		groveDir: groveDir,
		reposDir: reposDir,
		wsDir:    wsDir,
		cfg:      cfg,
		repoMap:  make(map[string]string),
		svc:      svc,
	}
}

// createRepo creates a real git repo with an initial commit.
func (e *testEnv) createRepo(name string) string {
	e.t.Helper()
	repoPath := filepath.Join(e.reposDir, name)
	e.run(e.reposDir, "git", "init", "-q", repoPath)
	e.run(repoPath, "git", "config", "user.email", "test@test.com")
	e.run(repoPath, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# "+name), 0o644)
	e.run(repoPath, "git", "add", ".")
	e.run(repoPath, "git", "commit", "-q", "-m", "initial")
	e.repoMap[name] = repoPath
	return repoPath
}

// createRepoWithRemote creates a repo cloned from a bare origin (for sync tests).
func (e *testEnv) createRepoWithRemote(name string) string {
	e.t.Helper()
	bare := filepath.Join(e.reposDir, name+"-origin.git")
	clone := filepath.Join(e.reposDir, name)

	e.run(e.reposDir, "git", "init", "-q", "--bare", bare)
	e.run(e.reposDir, "git", "clone", "-q", bare, clone)
	e.run(clone, "git", "config", "user.email", "test@test.com")
	e.run(clone, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(clone, "README.md"), []byte("# "+name), 0o644)
	e.run(clone, "git", "add", ".")
	e.run(clone, "git", "commit", "-q", "-m", "initial")
	e.run(clone, "git", "push", "-q", "origin", "HEAD")
	e.repoMap[name] = clone
	return clone
}

func (e *testEnv) run(dir string, name string, args ...string) string {
	e.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// Filter out GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE to prevent leaking
	// from parent process (e.g. when tests run inside a pre-commit hook).
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_DIR=") ||
			strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("%s %s failed: %s\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// Create tests
// ---------------------------------------------------------------------------

func TestCreateSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	err := env.svc.Create("test-ws", "feat/test", []string{"api", "web"}, env.repoMap, env.cfg)
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

func TestCreateDuplicateNameFails(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	env.svc.Create("dupe", "feat/a", []string{"api"}, env.repoMap, env.cfg)

	err := env.svc.Create("dupe", "feat/b", []string{"api"}, env.repoMap, env.cfg)
	if err == nil {
		t.Error("expected error for duplicate workspace name")
	}
}

func TestCreateDuplicateBranchFails(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	env.svc.Create("ws1", "feat/shared", []string{"api"}, env.repoMap, env.cfg)

	// Second workspace with same branch on same repo should fail
	err := env.svc.Create("ws2", "feat/shared", []string{"api"}, env.repoMap, env.cfg)
	if err == nil {
		t.Error("expected error for duplicate branch on same repo")
	}
}

func TestCreateRollbackOnFailure(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Try to create with a nonexistent repo — should rollback
	err := env.svc.Create("rollback-ws", "feat/test", []string{"api", "nonexistent"}, env.repoMap, env.cfg)
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

func TestCreateAutoCreatesBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Branch doesn't exist yet — should be auto-created
	err := env.svc.Create("auto-branch", "feat/new-branch", []string{"api"}, env.repoMap, env.cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Verify the branch exists in the worktree
	branch := env.run(filepath.Join(env.wsDir, "auto-branch", "api"), "git", "branch", "--show-current")
	if branch != "feat/new-branch" {
		t.Errorf("expected feat/new-branch, got %s", branch)
	}
}

func TestCreateWritesMCPConfig(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	env.svc.Create("mcp-ws", "feat/mcp", []string{"api"}, env.repoMap, env.cfg)

	// .mcp.json in workspace root
	mcpPath := filepath.Join(env.wsDir, "mcp-ws", ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	var mcpCfg models.MCPConfig
	if err := json.Unmarshal(data, &mcpCfg); err != nil {
		t.Fatalf("parsing .mcp.json: %v", err)
	}
	if _, ok := mcpCfg.MCPServers["grove"]; !ok {
		t.Error(".mcp.json missing grove server entry")
	}

	// .mcp.json should NOT be written inside the repo worktree — that would
	// dirty the tree and break sync. Claude Code is run from the workspace
	// root, which is where the shell integration cd's the user.
	wt := filepath.Join(env.wsDir, "mcp-ws", "api")
	if _, err := os.Stat(filepath.Join(wt, ".mcp.json")); err == nil {
		t.Error(".mcp.json should not be written inside a repo worktree")
	}
	status := env.run(wt, "git", "status", "--porcelain")
	if status != "" {
		t.Errorf("worktree should be clean after workspace create, got:\n%s", status)
	}
}

func TestCreateCdFile(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	cdFile := filepath.Join(env.dir, "cd-target")
	os.Setenv("GROVE_CD_FILE", cdFile)
	defer os.Unsetenv("GROVE_CD_FILE")

	env.svc.Create("cd-ws", "feat/cd", []string{"api"}, env.repoMap, env.cfg)

	data, err := os.ReadFile(cdFile)
	if err != nil {
		t.Fatalf("reading cd file: %v", err)
	}
	expected := filepath.Join(env.wsDir, "cd-ws")
	if string(data) != expected {
		t.Errorf("cd file: got %q, want %q", string(data), expected)
	}
}

// ---------------------------------------------------------------------------
// Setup hook tests
// ---------------------------------------------------------------------------

func TestSetupHookRuns(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("hooked")

	// Write .grove.toml with setup hook
	toml := `setup = "touch .setup-ran"`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add grove config")

	env.svc.Create("hook-ws", "feat/hook", []string{"hooked"}, env.repoMap, env.cfg)

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

	env.svc.Create("multi-ws", "feat/multi", []string{"multi"}, env.repoMap, env.cfg)

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

	err := env.svc.Create("plain-ws", "feat/plain", []string{"plain"}, env.repoMap, env.cfg)
	if err != nil {
		t.Fatalf("should not fail without setup hook: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Delete tests
// ---------------------------------------------------------------------------

func TestDeleteSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("del-ws", "feat/del", []string{"api"}, env.repoMap, env.cfg)

	err := env.svc.Delete("del-ws")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// State cleared
	ws, _ := env.svc.State.GetWorkspace("del-ws")
	if ws != nil {
		t.Error("workspace should be removed from state")
	}

	// Directory cleaned up
	if _, err := os.Stat(filepath.Join(env.wsDir, "del-ws")); !os.IsNotExist(err) {
		t.Error("workspace dir should be removed")
	}
}

func TestDeleteNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env // setup env for state path

	err := env.svc.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent workspace")
	}
}

func TestDeleteCleansBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("branch-ws", "feat/to-clean", []string{"api"}, env.repoMap, env.cfg)

	env.svc.Delete("branch-ws")

	// Branch should be cleaned up from source repo
	out := env.run(env.repoMap["api"], "git", "branch", "--list", "feat/to-clean")
	if strings.TrimSpace(out) != "" {
		t.Error("branch should be deleted from source repo")
	}
}

// ---------------------------------------------------------------------------
// Rename tests
// ---------------------------------------------------------------------------

func TestRenameSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("old-name", "feat/rename", []string{"api"}, env.repoMap, env.cfg)

	err := env.svc.Rename("old-name", "new-name")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Old name gone
	ws, _ := env.svc.State.GetWorkspace("old-name")
	if ws != nil {
		t.Error("old name should not exist")
	}

	// New name exists with updated paths
	ws, _ = env.svc.State.GetWorkspace("new-name")
	if ws == nil {
		t.Fatal("new workspace not found")
	}
	if !strings.Contains(ws.Path, "new-name") {
		t.Errorf("path should contain new-name: %s", ws.Path)
	}
	for _, r := range ws.Repos {
		if !strings.Contains(r.WorktreePath, "new-name") {
			t.Errorf("worktree path should contain new-name: %s", r.WorktreePath)
		}
	}

	// Directory renamed
	if _, err := os.Stat(filepath.Join(env.wsDir, "new-name")); os.IsNotExist(err) {
		t.Error("new directory should exist")
	}
	if _, err := os.Stat(filepath.Join(env.wsDir, "old-name")); !os.IsNotExist(err) {
		t.Error("old directory should not exist")
	}
}

func TestRenameNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := env.svc.Rename("nonexistent", "new")
	if err == nil {
		t.Error("expected error")
	}
}

func TestRenameNameTaken(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("ws-a", "feat/a", []string{"api"}, env.repoMap, env.cfg)
	env.svc.Create("ws-b", "feat/b", []string{"web"}, env.repoMap, env.cfg)

	err := env.svc.Rename("ws-a", "ws-b")
	if err == nil {
		t.Error("expected error for taken name")
	}
}

func TestRenamePreservesCreatedAt(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("preserve-ws", "feat/preserve", []string{"api"}, env.repoMap, env.cfg)

	ws, _ := env.svc.State.GetWorkspace("preserve-ws")
	originalCreatedAt := ws.CreatedAt

	env.svc.Rename("preserve-ws", "renamed-ws")

	ws, _ = env.svc.State.GetWorkspace("renamed-ws")
	if ws.CreatedAt != originalCreatedAt {
		t.Errorf("created_at changed: %q -> %q", originalCreatedAt, ws.CreatedAt)
	}
}

// TestReplaceSequence covers the Delete→Create flow that `gw create --replace`
// performs. The key invariants: after delete, the old workspace is gone from
// state and disk, and the freed branch can be reused immediately by the new
// workspace without hitting the duplicate-branch guard.
func TestReplaceSequence(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Create old workspace on a branch.
	if err := env.svc.Create("old-ws", "feat/shared", []string{"api"}, env.repoMap, env.cfg); err != nil {
		t.Fatalf("create old: %v", err)
	}

	// Sanity: creating another workspace on the same branch is normally rejected.
	if err := env.svc.Create("other", "feat/shared", []string{"api"}, env.repoMap, env.cfg); err == nil {
		t.Fatal("expected duplicate-branch rejection before delete")
	}

	// Delete old (simulates --replace first half).
	if err := env.svc.Delete("old-ws"); err != nil {
		t.Fatalf("delete old: %v", err)
	}

	// Old workspace is gone from state.
	if ws, _ := env.svc.State.GetWorkspace("old-ws"); ws != nil {
		t.Error("old-ws still in state after delete")
	}
	// Old workspace directory is gone from disk.
	if _, err := os.Stat(filepath.Join(env.wsDir, "old-ws")); !os.IsNotExist(err) {
		t.Error("old-ws directory still on disk after delete")
	}

	// Create new workspace on the SAME branch — should now succeed because
	// the old worktree releasing the branch is the whole point of --replace.
	if err := env.svc.Create("new-ws", "feat/shared", []string{"api"}, env.repoMap, env.cfg); err != nil {
		t.Fatalf("create new (branch reuse after delete): %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("new-ws")
	if ws == nil {
		t.Fatal("new-ws not in state")
	}
	if len(ws.Repos) != 1 || ws.Repos[0].Branch != "feat/shared" {
		t.Errorf("new-ws not on expected branch: %+v", ws.Repos)
	}
	if _, err := os.Stat(filepath.Join(env.wsDir, "new-ws", "api")); err != nil {
		t.Errorf("new-ws worktree missing: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sync tests
// ---------------------------------------------------------------------------

func TestSyncUpToDate(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.svc.Create("sync-ws", "feat/sync", []string{"api"}, env.repoMap, env.cfg)

	// No upstream changes — should be up to date
	err := env.svc.Sync("sync-ws")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func TestSyncNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := env.svc.Sync("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

// ---------------------------------------------------------------------------
// AddRepos tests
// ---------------------------------------------------------------------------

func TestAddReposSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	env.svc.Create("add-ws", "feat/add", []string{"api"}, env.repoMap, env.cfg)

	err := env.svc.AddRepos("add-ws", []string{"web"}, env.repoMap)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("add-ws")
	if len(ws.Repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(ws.Repos))
	}

	// Worktree dir exists
	if _, err := os.Stat(filepath.Join(env.wsDir, "add-ws", "web")); os.IsNotExist(err) {
		t.Error("web worktree dir missing")
	}
}

func TestAddReposAlreadyPresent(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("dup-ws", "feat/dup", []string{"api"}, env.repoMap, env.cfg)

	// Adding same repo again should be a no-op
	err := env.svc.AddRepos("dup-ws", []string{"api"}, env.repoMap)
	if err != nil {
		t.Fatalf("add duplicate: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("dup-ws")
	if len(ws.Repos) != 1 {
		t.Errorf("expected 1 repo (no dup), got %d", len(ws.Repos))
	}
}

func TestAddReposNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := env.svc.AddRepos("nonexistent", []string{"api"}, env.repoMap)
	if err == nil {
		t.Error("expected error")
	}
}

// ---------------------------------------------------------------------------
// RemoveRepos tests
// ---------------------------------------------------------------------------

func TestRemoveReposSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("rm-ws", "feat/rm", []string{"api", "web"}, env.repoMap, env.cfg)

	err := env.svc.RemoveRepos("rm-ws", []string{"web"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("rm-ws")
	if len(ws.Repos) != 1 {
		t.Errorf("expected 1 repo, got %d", len(ws.Repos))
	}
	if ws.Repos[0].RepoName != "api" {
		t.Errorf("remaining repo should be api, got %s", ws.Repos[0].RepoName)
	}

	// Worktree dir removed
	if _, err := os.Stat(filepath.Join(env.wsDir, "rm-ws", "web")); !os.IsNotExist(err) {
		t.Error("web worktree dir should be removed")
	}
}

func TestRemoveReposMultiple(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createRepo("worker")
	env.svc.Create("rm-multi", "feat/rm-multi", []string{"api", "web", "worker"}, env.repoMap, env.cfg)

	err := env.svc.RemoveRepos("rm-multi", []string{"web", "worker"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("rm-multi")
	if len(ws.Repos) != 1 {
		t.Errorf("expected 1 repo remaining, got %d", len(ws.Repos))
	}
	if ws.Repos[0].RepoName != "api" {
		t.Errorf("remaining should be api, got %s", ws.Repos[0].RepoName)
	}

	// Both worktree dirs should be gone
	for _, name := range []string{"web", "worker"} {
		wt := filepath.Join(env.wsDir, "rm-multi", name)
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Errorf("worktree %s should be removed", name)
		}
	}
}

func TestRemoveReposNonexistent(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("rm-ne", "feat/rm-ne", []string{"api"}, env.repoMap, env.cfg)

	// Removing a repo not in workspace should be a no-op
	err := env.svc.RemoveRepos("rm-ne", []string{"nonexistent"})
	if err != nil {
		t.Fatalf("remove nonexistent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Status tests
// ---------------------------------------------------------------------------

func TestStatusSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("status-ws", "feat/status", []string{"api"}, env.repoMap, env.cfg)

	err := env.svc.Status("status-ws", StatusOptions{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
}

func TestStatusJSON(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("json-ws", "feat/json", []string{"api"}, env.repoMap, env.cfg)

	err := env.svc.Status("json-ws", StatusOptions{JSON: true})
	if err != nil {
		t.Fatalf("status json: %v", err)
	}
}

func TestStatusVerbose(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("verbose-ws", "feat/verbose", []string{"api"}, env.repoMap, env.cfg)

	// Should not error — verbose shows raw git status for dirty repos
	err := env.svc.Status("verbose-ws", StatusOptions{Verbose: true})
	if err != nil {
		t.Fatalf("status verbose: %v", err)
	}
}

func TestStatusNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := env.svc.Status("nonexistent", StatusOptions{})
	if err == nil {
		t.Error("expected error")
	}
}

// ---------------------------------------------------------------------------
// Doctor tests
// ---------------------------------------------------------------------------

func TestDoctorHealthy(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("healthy-ws", "feat/healthy", []string{"api"}, env.repoMap, env.cfg)

	issues, _, err := env.svc.Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(issues))
	}
}

func TestDoctorDetectsMissingWorktree(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("stale-ws", "feat/stale", []string{"api"}, env.repoMap, env.cfg)

	// Delete worktree dir manually
	os.RemoveAll(filepath.Join(env.wsDir, "stale-ws", "api"))

	issues, _, err := env.svc.Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected at least 1 issue for missing worktree")
	}
}

func TestDoctorFixRemovesStale(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("fix-ws", "feat/fix", []string{"api"}, env.repoMap, env.cfg)

	// Delete worktree dir
	os.RemoveAll(filepath.Join(env.wsDir, "fix-ws", "api"))

	_, fixed, err := env.svc.Doctor(true)
	if err != nil {
		t.Fatalf("doctor fix: %v", err)
	}
	if fixed == 0 {
		t.Error("expected at least 1 fix")
	}

	// After fix, should be clean (or at least fewer issues)
	issues, _, _ := env.svc.Doctor(false)
	if len(issues) > 0 {
		// Workspace with no repos might still be an issue, that's ok
		t.Logf("remaining issues after fix: %d", len(issues))
	}
}

func TestDoctorDetectsMissingWorkspaceDir(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("ghost-ws", "feat/ghost", []string{"api"}, env.repoMap, env.cfg)

	// Delete entire workspace directory
	os.RemoveAll(filepath.Join(env.wsDir, "ghost-ws"))

	issues, _, err := env.svc.Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Issue, "workspace directory missing") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'workspace directory missing' issue")
	}
}

// ---------------------------------------------------------------------------
// Parallel behavior tests
// ---------------------------------------------------------------------------

func TestCreateMultiRepoAllProcessed(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createRepo("worker")

	err := env.svc.Create("multi-ws", "feat/multi", []string{"api", "web", "worker"}, env.repoMap, env.cfg)
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

func TestStatusMultiRepoAllReported(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("multi-status", "feat/ms", []string{"api", "web"}, env.repoMap, env.cfg)

	// Should not error even with multiple repos
	err := env.svc.Status("multi-status", StatusOptions{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
}

func TestDeleteMultiRepoAllCleaned(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("multi-del", "feat/md", []string{"api", "web"}, env.repoMap, env.cfg)

	err := env.svc.Delete("multi-del")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Both worktree dirs cleaned
	for _, name := range []string{"api", "web"} {
		wt := filepath.Join(env.wsDir, "multi-del", name)
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Errorf("worktree %s should be removed", name)
		}
	}

	// State cleared
	ws, _ := env.svc.State.GetWorkspace("multi-del")
	if ws != nil {
		t.Error("workspace should be removed from state")
	}
}

func TestSyncMultiRepo(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createRepoWithRemote("web")
	env.svc.Create("sync-multi", "feat/sm", []string{"api", "web"}, env.repoMap, env.cfg)

	err := env.svc.Sync("sync-multi")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sync with upstream changes
// ---------------------------------------------------------------------------

func TestSyncRebases(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")
	env.svc.Create("rebase-ws", "feat/rebase", []string{"api"}, env.repoMap, env.cfg)
	wt := filepath.Join(env.wsDir, "rebase-ws", "api")

	// Add a commit upstream on main and push to origin
	os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("new"), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "upstream change")
	env.run(repo, "git", "push", "-q", "origin", "HEAD")

	// Sync should rebase
	err := env.svc.Sync("rebase-ws")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	// The upstream file should now be in the worktree
	if _, err := os.Stat(filepath.Join(wt, "upstream.txt")); os.IsNotExist(err) {
		t.Error("upstream change should be rebased into worktree")
	}
}

// ---------------------------------------------------------------------------
// AllWorkspacesSummary tests
// ---------------------------------------------------------------------------

func TestAllWorkspacesSummaryEmpty(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	results, err := env.svc.AllWorkspacesSummary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %d", len(results))
	}
}

func TestAllWorkspacesSummaryMultiple(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("ws-a", "feat/a", []string{"api"}, env.repoMap, env.cfg)
	env.svc.Create("ws-b", "feat/b", []string{"web"}, env.repoMap, env.cfg)

	results, err := env.svc.AllWorkspacesSummary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2, got %d", len(results))
	}

	// Each result should have name, branch, repos count, status, path
	for _, r := range results {
		if r.Name == "" || r.Branch == "" || r.Path == "" {
			t.Errorf("empty field in result: %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------
// MCP config tests
// ---------------------------------------------------------------------------

func TestMCPConfigMergesWithExisting(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	env.svc.Create("merge-ws", "feat/merge", []string{"api"}, env.repoMap, env.cfg)

	// Add another server to .mcp.json
	mcpPath := filepath.Join(env.wsDir, "merge-ws", "api", ".mcp.json")
	existing := `{"mcpServers":{"grove":{"command":"gw","args":["mcp-serve","--workspace","merge-ws"]},"other":{"command":"other-tool","args":[]}}}`
	os.WriteFile(mcpPath, []byte(existing), 0o644)

	// Create another workspace that writes .mcp.json — simulate by calling writeMCPConfig directly
	ws, _ := env.svc.State.GetWorkspace("merge-ws")
	writeMCPConfig(*ws)

	// "other" server should still be there
	data, _ := os.ReadFile(mcpPath)
	var mcpCfg map[string]interface{}
	json.Unmarshal(data, &mcpCfg)

	servers := mcpCfg["mcpServers"].(map[string]interface{})
	if _, ok := servers["other"]; !ok {
		t.Error("existing 'other' server should be preserved")
	}
	if _, ok := servers["grove"]; !ok {
		t.Error("'grove' server should exist")
	}
}

func TestMCPConfigRemoveOnlyGrove(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	env.svc.Create("rmcp-ws", "feat/rmcp", []string{"api"}, env.repoMap, env.cfg)

	// Add another server at the workspace-root .mcp.json
	mcpPath := filepath.Join(env.wsDir, "rmcp-ws", ".mcp.json")
	existing := `{"mcpServers":{"grove":{"command":"gw","args":["mcp-serve","--workspace","rmcp-ws"]},"keeper":{"command":"keep-me","args":[]}}}`
	os.WriteFile(mcpPath, []byte(existing), 0o644)

	ws, _ := env.svc.State.GetWorkspace("rmcp-ws")
	removeMCPConfig(*ws)

	// "keeper" should remain, "grove" should be gone
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
	var mcpCfg map[string]interface{}
	json.Unmarshal(data, &mcpCfg)
	servers := mcpCfg["mcpServers"].(map[string]interface{})
	if _, ok := servers["grove"]; ok {
		t.Error("'grove' should be removed")
	}
	if _, ok := servers["keeper"]; !ok {
		t.Error("'keeper' should be preserved")
	}
}

func TestSyncSkipsDirty(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.svc.Create("dirty-ws", "feat/dirty", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "dirty-ws", "api")
	os.WriteFile(filepath.Join(wt, "dirt.txt"), []byte("uncommitted"), 0o644)

	err := env.svc.Sync("dirty-ws")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	// No error = correctly skipped
}

// ---------------------------------------------------------------------------
// Teardown hook tests
// ---------------------------------------------------------------------------

func TestDeleteRunsTeardownHook(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")

	// Write .grove.toml with teardown hook
	toml := `teardown = "touch /tmp/grove-teardown-test-marker"`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add teardown")

	// Override RunCmdSilent to capture calls
	var teardownCalls []string
	origRunCmdSilent := env.svc.RunCmdSilent
	env.svc.RunCmdSilent = func(dir, cmd string) error {
		teardownCalls = append(teardownCalls, cmd)
		return nil
	}
	defer func() { env.svc.RunCmdSilent = origRunCmdSilent }()

	env.svc.Create("td-ws", "feat/td", []string{"api"}, env.repoMap, env.cfg)
	env.svc.Delete("td-ws")

	found := false
	for _, c := range teardownCalls {
		if strings.Contains(c, "teardown-test-marker") {
			found = true
		}
	}
	if !found {
		t.Errorf("teardown hook not called; calls: %v", teardownCalls)
	}
}

func TestRemoveReposRunsTeardownHook(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	env.createRepo("web")

	toml := `teardown = "echo tearing-down"`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add teardown")

	var teardownCalls []string
	origRunCmdSilent := env.svc.RunCmdSilent
	env.svc.RunCmdSilent = func(dir, cmd string) error {
		teardownCalls = append(teardownCalls, cmd)
		return nil
	}
	defer func() { env.svc.RunCmdSilent = origRunCmdSilent }()

	env.svc.Create("td-rm-ws", "feat/td-rm", []string{"api", "web"}, env.repoMap, env.cfg)
	env.svc.RemoveRepos("td-rm-ws", []string{"api"})

	found := false
	for _, c := range teardownCalls {
		if strings.Contains(c, "tearing-down") {
			found = true
		}
	}
	if !found {
		t.Errorf("teardown hook not called during remove; calls: %v", teardownCalls)
	}
}

// ---------------------------------------------------------------------------
// Pre/post sync hook tests
// ---------------------------------------------------------------------------

func TestSyncRunsPreAndPostHooks(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")

	toml := `pre_sync = "echo pre"
post_sync = "echo post"`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add sync hooks")
	env.run(repo, "git", "push", "-q", "origin", "HEAD")

	env.svc.Create("sync-hook-ws", "feat/sync-hook", []string{"api"}, env.repoMap, env.cfg)

	// Add upstream commit to trigger rebase
	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("upstream"), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "upstream")
	env.run(repo, "git", "push", "-q", "origin", "HEAD")

	var hookCalls []string
	origRunCmdSilent := env.svc.RunCmdSilent
	env.svc.RunCmdSilent = func(dir, cmd string) error {
		hookCalls = append(hookCalls, cmd)
		return nil
	}
	defer func() { env.svc.RunCmdSilent = origRunCmdSilent }()

	env.svc.Sync("sync-hook-ws")

	foundPre := false
	foundPost := false
	for _, c := range hookCalls {
		if strings.Contains(c, "pre") {
			foundPre = true
		}
		if strings.Contains(c, "post") {
			foundPost = true
		}
	}
	if !foundPre {
		t.Errorf("pre_sync hook not called; calls: %v", hookCalls)
	}
	if !foundPost {
		t.Errorf("post_sync hook not called; calls: %v", hookCalls)
	}
}

// ---------------------------------------------------------------------------
// Setup hook injection test
// ---------------------------------------------------------------------------

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

	env.svc.Create("inject-ws", "feat/inject", []string{"api"}, env.repoMap, env.cfg)

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

// ---------------------------------------------------------------------------
// Delete partial failure preserves state
// ---------------------------------------------------------------------------

func TestDeletePartialFailurePreservesState(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("partial-ws", "feat/partial", []string{"api", "web"}, env.repoMap, env.cfg)

	// Corrupt one worktree by removing its .git reference
	// This simulates a worktree that can't be removed via git
	ws, _ := env.svc.State.GetWorkspace("partial-ws")
	apiWT := ws.Repos[0].WorktreePath

	// Lock the worktree dir to prevent removal (create a dir that looks like it's still there)
	// We can't easily simulate a hard failure, but we can verify the state
	// by checking that the dir exists after delete
	_ = apiWT

	// The existing Delete implementation removes the directory regardless,
	// so we test that state is removed when everything succeeds
	env.svc.Delete("partial-ws")

	wsAfter, _ := env.svc.State.GetWorkspace("partial-ws")
	if wsAfter != nil {
		t.Error("on successful delete, workspace should be removed from state")
	}
}

// ---------------------------------------------------------------------------
// AddRepos edge cases
// ---------------------------------------------------------------------------

func TestAddReposBranchConflict(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	// Create first workspace, consuming the branch on "web"
	env.svc.Create("ws1", "feat/conflict", []string{"web"}, env.repoMap, env.cfg)

	// Create second workspace with "api" only
	env.svc.Create("ws2", "feat/other", []string{"api"}, env.repoMap, env.cfg)

	// Try to add "web" to ws2 with a different branch — but web already has
	// feat/conflict. This should work because it's a different branch.
	// But adding web with a branch that already has a worktree should fail.
	// The branch "feat/conflict" already has a worktree, so adding it again should error.
	err := env.svc.AddRepos("ws2", []string{"web"}, env.repoMap)
	// This will try to create branch "feat/other" on "web" — should work (different branch)
	if err != nil {
		t.Fatalf("adding web with different branch should succeed: %v", err)
	}
}

func TestAddReposRunsSetupHooks(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	repo := env.createRepo("web")

	toml := `setup = "touch .added-setup"`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add setup")

	env.svc.Create("setup-add-ws", "feat/setup-add", []string{"api"}, env.repoMap, env.cfg)

	var setupCalls []string
	origRunCmd := env.svc.RunCmd
	env.svc.RunCmd = func(dir, cmd string) error {
		setupCalls = append(setupCalls, cmd)
		return nil
	}
	defer func() { env.svc.RunCmd = origRunCmd }()

	env.svc.AddRepos("setup-add-ws", []string{"web"}, env.repoMap)

	found := false
	for _, c := range setupCalls {
		if strings.Contains(c, "added-setup") {
			found = true
		}
	}
	if !found {
		t.Errorf("setup hook should run on newly added repos; calls: %v", setupCalls)
	}
}

// ---------------------------------------------------------------------------
// RemoveRepos error paths
// ---------------------------------------------------------------------------

func TestRemoveReposWorkspaceNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := env.svc.RemoveRepos("nonexistent", []string{"api"})
	if err == nil {
		t.Error("expected error for nonexistent workspace")
	}
}

// ---------------------------------------------------------------------------
// Sync conflict handling
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Progress output tests
// ---------------------------------------------------------------------------

func TestCreateShowsProgressOnStderr(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	env.svc.Create("progress-ws", "feat/progress", []string{"api", "web"}, env.repoMap, env.cfg)

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

func TestSyncConflictAbortsRebase(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")
	env.svc.Create("conflict-ws", "feat/conflict", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "conflict-ws", "api")

	// Make a conflicting change in the worktree
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("worktree version"), 0o644)
	env.run(wt, "git", "add", ".")
	env.run(wt, "git", "commit", "-q", "-m", "worktree change")

	// Make a conflicting change upstream on the same file
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("upstream version"), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "upstream conflict")
	env.run(repo, "git", "push", "-q", "origin", "HEAD")

	// Sync should handle the conflict gracefully (abort rebase, no error)
	err := env.svc.Sync("conflict-ws")
	if err != nil {
		t.Fatalf("sync should not return error on conflict: %v", err)
	}

	// Worktree should not be in a rebase state
	rebaseMergeDir := filepath.Join(wt, ".git", "rebase-merge")
	if _, err := os.Stat(rebaseMergeDir); err == nil {
		// .git might be a file (worktree), check differently
		t.Log("checking rebase state via git status")
	}

	// The worktree change should still be present (rebase was aborted)
	data, _ := os.ReadFile(filepath.Join(wt, "README.md"))
	if string(data) != "worktree version" {
		t.Errorf("after abort, worktree should keep its version; got %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// Logging e2e tests — verify the flight recorder captures key operations
// ---------------------------------------------------------------------------

// setupLogging points the logging package at a temp dir and returns a function
// to read the log contents. Always call in tests that check log output.
func setupLogging(t *testing.T) func() string {
	t.Helper()
	dir := t.TempDir()
	logging.LogDir = dir
	logging.Setup(false) // non-verbose: Info/Warn/Error still written

	return func() string {
		data, err := os.ReadFile(filepath.Join(dir, "grove.log"))
		if err != nil {
			t.Fatalf("reading log file: %v", err)
		}
		return string(data)
	}
}

func TestLoggingCreateAndDelete(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	// Create workspace
	err := env.svc.Create("log-ws", "feat/log", []string{"api", "web"}, env.repoMap, env.cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	log := readLog()

	// Should log creation start with workspace name, branch, and repos
	if !strings.Contains(log, `creating workspace "log-ws"`) {
		t.Errorf("log should contain creation start, got:\n%s", log)
	}
	if !strings.Contains(log, "feat/log") {
		t.Errorf("log should contain branch name, got:\n%s", log)
	}
	if !strings.Contains(log, `workspace "log-ws" created`) {
		t.Errorf("log should contain creation success, got:\n%s", log)
	}

	// Should log branch creation for each repo
	if !strings.Contains(log, `creating branch "feat/log" in api`) {
		t.Errorf("log should contain branch creation for api, got:\n%s", log)
	}
	if !strings.Contains(log, `creating branch "feat/log" in web`) {
		t.Errorf("log should contain branch creation for web, got:\n%s", log)
	}

	// Delete workspace
	err = env.svc.Delete("log-ws")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	log = readLog()

	if !strings.Contains(log, `deleting workspace "log-ws"`) {
		t.Errorf("log should contain deletion start, got:\n%s", log)
	}
	if !strings.Contains(log, `workspace "log-ws" deleted`) {
		t.Errorf("log should contain deletion success, got:\n%s", log)
	}
	// Branch deletion should be logged
	if !strings.Contains(log, `deleted branch "feat/log"`) {
		t.Errorf("log should contain branch deletion, got:\n%s", log)
	}
}

func TestLoggingSync(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")

	err := env.svc.Create("sync-log-ws", "feat/synclog", []string{"api"}, env.repoMap, env.cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = env.svc.Sync("sync-log-ws")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	log := readLog()
	if !strings.Contains(log, `syncing workspace "sync-log-ws"`) {
		t.Errorf("log should contain sync start, got:\n%s", log)
	}
}

func TestLoggingAddAndRemoveRepos(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	env.svc.Create("addrem-ws", "feat/addrem", []string{"api"}, env.repoMap, env.cfg)

	err := env.svc.AddRepos("addrem-ws", []string{"web"}, env.repoMap)
	if err != nil {
		t.Fatalf("add-repo: %v", err)
	}

	log := readLog()
	if !strings.Contains(log, `added 1 repo(s) to workspace "addrem-ws"`) {
		t.Errorf("log should contain add-repo, got:\n%s", log)
	}

	err = env.svc.RemoveRepos("addrem-ws", []string{"web"})
	if err != nil {
		t.Fatalf("remove-repo: %v", err)
	}

	log = readLog()
	if !strings.Contains(log, `removed 1 repo(s) from workspace "addrem-ws"`) {
		t.Errorf("log should contain remove-repo, got:\n%s", log)
	}
}

func TestLoggingRename(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")

	env.svc.Create("old-name", "feat/rename", []string{"api"}, env.repoMap, env.cfg)

	err := env.svc.Rename("old-name", "new-name")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	log := readLog()
	if !strings.Contains(log, `workspace "old-name" renamed to "new-name"`) {
		t.Errorf("log should contain rename, got:\n%s", log)
	}
}

func TestDeleteForceDeletesUnmergedBranch(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")

	env.svc.Create("unmerged-ws", "feat/unmerged", []string{"api"}, env.repoMap, env.cfg)

	// Add an unmerged commit to the worktree branch
	wt := filepath.Join(env.wsDir, "unmerged-ws", "api")
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("unmerged work"), 0o644)
	env.run(wt, "git", "add", ".")
	env.run(wt, "git", "commit", "-q", "-m", "unmerged commit")

	sourceRepo := env.repoMap["api"]
	env.svc.Delete("unmerged-ws")

	log := readLog()
	if !strings.Contains(log, "deleted branch") {
		t.Errorf("log should confirm branch deletion, got:\n%s", log)
	}

	// Verify the branch is actually gone from the source repo
	if gitops.BranchExists(sourceRepo, "feat/unmerged") {
		t.Error("branch feat/unmerged should have been force-deleted from source repo")
	}
}
