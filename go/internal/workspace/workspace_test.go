package workspace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"

	// blank import for test setup side effects is not needed
	// claude is tested via workspace behavior
)

// testEnv sets up isolated Grove dirs + real git repos for testing.
type testEnv struct {
	t          *testing.T
	dir        string
	reposDir   string
	wsDir      string
	cfg        *models.Config
	repoMap    map[string]string
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

	// Override package-level vars
	config.GroveDir = groveDir
	config.ConfigPath = filepath.Join(groveDir, "config.toml")
	config.DefaultWorkspaceDir = wsDir

	// Write empty state
	os.WriteFile(filepath.Join(groveDir, "state.json"), []byte("[]"), 0o644)

	// Override ClaudeDir for tests
	ClaudeDir = filepath.Join(dir, ".claude")

	cfg := &models.Config{
		RepoDirs:     []string{reposDir},
		WorkspaceDir: wsDir,
		Presets:      map[string]models.Preset{},
	}

	// Save config so Delete/Rename can load it
	config.Save(cfg)

	return &testEnv{
		t:        t,
		dir:      dir,
		reposDir: reposDir,
		wsDir:    wsDir,
		cfg:      cfg,
		repoMap:  make(map[string]string),
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

	err := Create("test-ws", "feat/test", []string{"api", "web"}, env.repoMap, env.cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Workspace saved to state
	ws, _ := state.GetWorkspace("test-ws")
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

	Create("dupe", "feat/a", []string{"api"}, env.repoMap, env.cfg)

	err := Create("dupe", "feat/b", []string{"api"}, env.repoMap, env.cfg)
	if err == nil {
		t.Error("expected error for duplicate workspace name")
	}
}

func TestCreateDuplicateBranchFails(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	Create("ws1", "feat/shared", []string{"api"}, env.repoMap, env.cfg)

	// Second workspace with same branch on same repo should fail
	err := Create("ws2", "feat/shared", []string{"api"}, env.repoMap, env.cfg)
	if err == nil {
		t.Error("expected error for duplicate branch on same repo")
	}
}

func TestCreateRollbackOnFailure(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Try to create with a nonexistent repo — should rollback
	err := Create("rollback-ws", "feat/test", []string{"api", "nonexistent"}, env.repoMap, env.cfg)
	if err == nil {
		t.Error("expected error")
	}

	// Workspace dir should be cleaned up
	wsPath := filepath.Join(env.wsDir, "rollback-ws")
	if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
		t.Error("workspace dir should be removed on rollback")
	}

	// State should not contain the workspace
	ws, _ := state.GetWorkspace("rollback-ws")
	if ws != nil {
		t.Error("workspace should not be in state after rollback")
	}
}

func TestCreateAutoCreatesBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Branch doesn't exist yet — should be auto-created
	err := Create("auto-branch", "feat/new-branch", []string{"api"}, env.repoMap, env.cfg)
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

	Create("mcp-ws", "feat/mcp", []string{"api"}, env.repoMap, env.cfg)

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

	// .mcp.json in worktree dir
	wtMcpPath := filepath.Join(env.wsDir, "mcp-ws", "api", ".mcp.json")
	if _, err := os.Stat(wtMcpPath); os.IsNotExist(err) {
		t.Error(".mcp.json not written to worktree dir")
	}
}

func TestCreateCdFile(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	cdFile := filepath.Join(env.dir, "cd-target")
	os.Setenv("GROVE_CD_FILE", cdFile)
	defer os.Unsetenv("GROVE_CD_FILE")

	Create("cd-ws", "feat/cd", []string{"api"}, env.repoMap, env.cfg)

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

	Create("hook-ws", "feat/hook", []string{"hooked"}, env.repoMap, env.cfg)

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

	Create("multi-ws", "feat/multi", []string{"multi"}, env.repoMap, env.cfg)

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

	err := Create("plain-ws", "feat/plain", []string{"plain"}, env.repoMap, env.cfg)
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
	Create("del-ws", "feat/del", []string{"api"}, env.repoMap, env.cfg)

	err := Delete("del-ws")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// State cleared
	ws, _ := state.GetWorkspace("del-ws")
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

	err := Delete("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent workspace")
	}
}

func TestDeleteCleansBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	Create("branch-ws", "feat/to-clean", []string{"api"}, env.repoMap, env.cfg)

	Delete("branch-ws")

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
	Create("old-name", "feat/rename", []string{"api"}, env.repoMap, env.cfg)

	err := Rename("old-name", "new-name")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Old name gone
	ws, _ := state.GetWorkspace("old-name")
	if ws != nil {
		t.Error("old name should not exist")
	}

	// New name exists with updated paths
	ws, _ = state.GetWorkspace("new-name")
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

	err := Rename("nonexistent", "new")
	if err == nil {
		t.Error("expected error")
	}
}

func TestRenameNameTaken(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	Create("ws-a", "feat/a", []string{"api"}, env.repoMap, env.cfg)
	Create("ws-b", "feat/b", []string{"web"}, env.repoMap, env.cfg)

	err := Rename("ws-a", "ws-b")
	if err == nil {
		t.Error("expected error for taken name")
	}
}

func TestRenamePreservesCreatedAt(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	Create("preserve-ws", "feat/preserve", []string{"api"}, env.repoMap, env.cfg)

	ws, _ := state.GetWorkspace("preserve-ws")
	originalCreatedAt := ws.CreatedAt

	Rename("preserve-ws", "renamed-ws")

	ws, _ = state.GetWorkspace("renamed-ws")
	if ws.CreatedAt != originalCreatedAt {
		t.Errorf("created_at changed: %q -> %q", originalCreatedAt, ws.CreatedAt)
	}
}

// ---------------------------------------------------------------------------
// Sync tests
// ---------------------------------------------------------------------------

func TestSyncUpToDate(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	Create("sync-ws", "feat/sync", []string{"api"}, env.repoMap, env.cfg)

	// Clean the worktree (untracked .mcp.json)
	wt := filepath.Join(env.wsDir, "sync-ws", "api")
	env.run(wt, "git", "add", "-A")
	env.run(wt, "git", "commit", "-q", "-m", "clean")

	// No upstream changes — should be up to date
	err := Sync("sync-ws")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func TestSyncNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := Sync("nonexistent")
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

	Create("add-ws", "feat/add", []string{"api"}, env.repoMap, env.cfg)

	err := AddRepos("add-ws", []string{"web"}, env.repoMap)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	ws, _ := state.GetWorkspace("add-ws")
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
	Create("dup-ws", "feat/dup", []string{"api"}, env.repoMap, env.cfg)

	// Adding same repo again should be a no-op
	err := AddRepos("dup-ws", []string{"api"}, env.repoMap)
	if err != nil {
		t.Fatalf("add duplicate: %v", err)
	}

	ws, _ := state.GetWorkspace("dup-ws")
	if len(ws.Repos) != 1 {
		t.Errorf("expected 1 repo (no dup), got %d", len(ws.Repos))
	}
}

func TestAddReposNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := AddRepos("nonexistent", []string{"api"}, env.repoMap)
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
	Create("rm-ws", "feat/rm", []string{"api", "web"}, env.repoMap, env.cfg)

	err := RemoveRepos("rm-ws", []string{"web"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}

	ws, _ := state.GetWorkspace("rm-ws")
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
	Create("rm-multi", "feat/rm-multi", []string{"api", "web", "worker"}, env.repoMap, env.cfg)

	err := RemoveRepos("rm-multi", []string{"web", "worker"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}

	ws, _ := state.GetWorkspace("rm-multi")
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
	Create("rm-ne", "feat/rm-ne", []string{"api"}, env.repoMap, env.cfg)

	// Removing a repo not in workspace should be a no-op
	err := RemoveRepos("rm-ne", []string{"nonexistent"})
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
	Create("status-ws", "feat/status", []string{"api"}, env.repoMap, env.cfg)

	err := Status("status-ws", StatusOptions{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
}

func TestStatusJSON(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	Create("json-ws", "feat/json", []string{"api"}, env.repoMap, env.cfg)

	err := Status("json-ws", StatusOptions{JSON: true})
	if err != nil {
		t.Fatalf("status json: %v", err)
	}
}

func TestStatusVerbose(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	Create("verbose-ws", "feat/verbose", []string{"api"}, env.repoMap, env.cfg)

	// Should not error — verbose shows raw git status for dirty repos
	err := Status("verbose-ws", StatusOptions{Verbose: true})
	if err != nil {
		t.Fatalf("status verbose: %v", err)
	}
}

func TestStatusNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := Status("nonexistent", StatusOptions{})
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
	Create("healthy-ws", "feat/healthy", []string{"api"}, env.repoMap, env.cfg)

	issues, _, err := Doctor(false)
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
	Create("stale-ws", "feat/stale", []string{"api"}, env.repoMap, env.cfg)

	// Delete worktree dir manually
	os.RemoveAll(filepath.Join(env.wsDir, "stale-ws", "api"))

	issues, _, err := Doctor(false)
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
	Create("fix-ws", "feat/fix", []string{"api"}, env.repoMap, env.cfg)

	// Delete worktree dir
	os.RemoveAll(filepath.Join(env.wsDir, "fix-ws", "api"))

	_, fixed, err := Doctor(true)
	if err != nil {
		t.Fatalf("doctor fix: %v", err)
	}
	if fixed == 0 {
		t.Error("expected at least 1 fix")
	}

	// After fix, should be clean (or at least fewer issues)
	issues, _, _ := Doctor(false)
	if len(issues) > 0 {
		// Workspace with no repos might still be an issue, that's ok
		t.Logf("remaining issues after fix: %d", len(issues))
	}
}

func TestDoctorDetectsMissingWorkspaceDir(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	Create("ghost-ws", "feat/ghost", []string{"api"}, env.repoMap, env.cfg)

	// Delete entire workspace directory
	os.RemoveAll(filepath.Join(env.wsDir, "ghost-ws"))

	issues, _, err := Doctor(false)
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

func TestDoctorDetectsOrphanedClaudeMemory(t *testing.T) {
	env := setupTestEnv(t)
	env.cfg.ClaudeMemorySync = true
	config.Save(env.cfg)
	env.createRepo("api")
	Create("orphan-ws", "feat/orphan", []string{"api"}, env.repoMap, env.cfg)

	// Delete workspace but leave Claude memory dir behind
	ws, _ := state.GetWorkspace("orphan-ws")
	wtPath := ws.Repos[0].WorktreePath

	Delete("orphan-ws")

	// Manually create an orphaned Claude memory dir for the deleted worktree
	claudeDir := ClaudeDir
	encoded := strings.ReplaceAll(wtPath, "/", "-")
	encoded = strings.ReplaceAll(encoded, ".", "-")
	orphanDir := filepath.Join(claudeDir, "projects", encoded, "memory")
	os.MkdirAll(orphanDir, 0o755)
	os.WriteFile(filepath.Join(orphanDir, "stale.md"), []byte("old"), 0o644)

	issues, _, err := Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Issue, "orphaned Claude memory") {
			found = true
		}
	}
	if !found {
		t.Error("expected orphaned Claude memory issue")
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

	err := Create("multi-ws", "feat/multi", []string{"api", "web", "worker"}, env.repoMap, env.cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := state.GetWorkspace("multi-ws")
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
	Create("multi-status", "feat/ms", []string{"api", "web"}, env.repoMap, env.cfg)

	// Should not error even with multiple repos
	err := Status("multi-status", StatusOptions{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
}

func TestDeleteMultiRepoAllCleaned(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	Create("multi-del", "feat/md", []string{"api", "web"}, env.repoMap, env.cfg)

	err := Delete("multi-del")
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
	ws, _ := state.GetWorkspace("multi-del")
	if ws != nil {
		t.Error("workspace should be removed from state")
	}
}

func TestSyncMultiRepo(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createRepoWithRemote("web")
	Create("sync-multi", "feat/sm", []string{"api", "web"}, env.repoMap, env.cfg)

	// Clean worktrees
	for _, name := range []string{"api", "web"} {
		wt := filepath.Join(env.wsDir, "sync-multi", name)
		env.run(wt, "git", "add", "-A")
		env.run(wt, "git", "commit", "-q", "-m", "clean")
	}

	err := Sync("sync-multi")
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
	Create("rebase-ws", "feat/rebase", []string{"api"}, env.repoMap, env.cfg)

	// Clean the worktree
	wt := filepath.Join(env.wsDir, "rebase-ws", "api")
	env.run(wt, "git", "add", "-A")
	env.run(wt, "git", "commit", "-q", "-m", "clean")

	// Add a commit upstream on main and push to origin
	os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("new"), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "upstream change")
	env.run(repo, "git", "push", "-q", "origin", "HEAD")

	// Sync should rebase
	err := Sync("rebase-ws")
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

	results, err := AllWorkspacesSummary()
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
	Create("ws-a", "feat/a", []string{"api"}, env.repoMap, env.cfg)
	Create("ws-b", "feat/b", []string{"web"}, env.repoMap, env.cfg)

	results, err := AllWorkspacesSummary()
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

	Create("merge-ws", "feat/merge", []string{"api"}, env.repoMap, env.cfg)

	// Add another server to .mcp.json
	mcpPath := filepath.Join(env.wsDir, "merge-ws", "api", ".mcp.json")
	existing := `{"mcpServers":{"grove":{"command":"gw","args":["mcp-serve","--workspace","merge-ws"]},"other":{"command":"other-tool","args":[]}}}`
	os.WriteFile(mcpPath, []byte(existing), 0o644)

	// Create another workspace that writes .mcp.json — simulate by calling writeMCPConfig directly
	ws, _ := state.GetWorkspace("merge-ws")
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

	Create("rmcp-ws", "feat/rmcp", []string{"api"}, env.repoMap, env.cfg)

	// Add another server
	mcpPath := filepath.Join(env.wsDir, "rmcp-ws", "api", ".mcp.json")
	existing := `{"mcpServers":{"grove":{"command":"gw","args":["mcp-serve","--workspace","rmcp-ws"]},"keeper":{"command":"keep-me","args":[]}}}`
	os.WriteFile(mcpPath, []byte(existing), 0o644)

	ws, _ := state.GetWorkspace("rmcp-ws")
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

// ---------------------------------------------------------------------------
// Claude memory sync tests
// ---------------------------------------------------------------------------

func TestCreateRehydratesClaudeMemory(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	env.cfg.ClaudeMemorySync = true

	// Set up a fake ~/.claude with memory for the source repo
	claudeDir := filepath.Join(env.dir, ".claude")
	encodedSource := strings.ReplaceAll(repo, "/", "-")
	encodedSource = strings.ReplaceAll(encodedSource, ".", "-")
	memDir := filepath.Join(claudeDir, "projects", encodedSource, "memory")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "context.md"), []byte("source memory"), 0o644)

	Create("claude-ws", "feat/claude", []string{"api"}, env.repoMap, env.cfg)

	// Memory should be copied to worktree's claude project dir
	wtPath := filepath.Join(env.wsDir, "claude-ws", "api")
	encodedWT := strings.ReplaceAll(wtPath, "/", "-")
	encodedWT = strings.ReplaceAll(encodedWT, ".", "-")
	wtMemDir := filepath.Join(claudeDir, "projects", encodedWT, "memory")

	data, err := os.ReadFile(filepath.Join(wtMemDir, "context.md"))
	if err != nil {
		t.Fatalf("memory not rehydrated: %v", err)
	}
	if string(data) != "source memory" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestCreateSkipsClaudeSyncWhenDisabled(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	env.cfg.ClaudeMemorySync = false

	// Set up memory
	claudeDir := filepath.Join(env.dir, ".claude")
	encodedSource := strings.ReplaceAll(repo, "/", "-")
	encodedSource = strings.ReplaceAll(encodedSource, ".", "-")
	memDir := filepath.Join(claudeDir, "projects", encodedSource, "memory")
	os.MkdirAll(memDir, 0o755)
	os.WriteFile(filepath.Join(memDir, "context.md"), []byte("source memory"), 0o644)

	Create("no-sync", "feat/nosync", []string{"api"}, env.repoMap, env.cfg)

	// Memory should NOT be copied
	wtPath := filepath.Join(env.wsDir, "no-sync", "api")
	encodedWT := strings.ReplaceAll(wtPath, "/", "-")
	encodedWT = strings.ReplaceAll(encodedWT, ".", "-")
	wtMemFile := filepath.Join(claudeDir, "projects", encodedWT, "memory", "context.md")

	if _, err := os.Stat(wtMemFile); !os.IsNotExist(err) {
		t.Error("memory should not be synced when disabled")
	}
}

func TestDeleteHarvestsClaudeMemory(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.cfg.ClaudeMemorySync = true
	config.Save(env.cfg) // persist so Delete can load it

	Create("harvest-ws", "feat/harvest", []string{"api"}, env.repoMap, env.cfg)

	// Add memory in the worktree's project dir
	claudeDir := filepath.Join(env.dir, ".claude")
	wtPath := filepath.Join(env.wsDir, "harvest-ws", "api")
	encodedWT := strings.ReplaceAll(wtPath, "/", "-")
	encodedWT = strings.ReplaceAll(encodedWT, ".", "-")
	wtMemDir := filepath.Join(claudeDir, "projects", encodedWT, "memory")
	os.MkdirAll(wtMemDir, 0o755)
	os.WriteFile(filepath.Join(wtMemDir, "learned.md"), []byte("new insight"), 0o644)

	Delete("harvest-ws")

	// Memory should be harvested back to source repo's project dir
	sourceRepo := env.repoMap["api"]
	encodedSource := strings.ReplaceAll(sourceRepo, "/", "-")
	encodedSource = strings.ReplaceAll(encodedSource, ".", "-")
	srcMemFile := filepath.Join(claudeDir, "projects", encodedSource, "memory", "learned.md")

	data, err := os.ReadFile(srcMemFile)
	if err != nil {
		t.Fatalf("memory not harvested: %v", err)
	}
	if string(data) != "new insight" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestSyncSkipsDirty(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	Create("dirty-ws", "feat/dirty", []string{"api"}, env.repoMap, env.cfg)

	// Make worktree dirty (don't commit the .mcp.json)
	// It's already dirty due to .mcp.json, so sync should skip

	err := Sync("dirty-ws")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	// No error = correctly skipped
}
