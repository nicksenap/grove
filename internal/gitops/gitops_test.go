package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// initBareRepo creates a bare git repo and a clone of it for testing.
// Returns (clonePath, barePath). The clone has an initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	bare := filepath.Join(dir, "origin.git")
	clone := filepath.Join(dir, "repo")

	// Create bare repo
	run(t, dir, "git", "init", "--bare", bare)

	// Clone it
	run(t, dir, "git", "clone", bare, clone)

	// Configure user
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")

	// Initial commit on main
	os.WriteFile(filepath.Join(clone, "README.md"), []byte("# test"), 0o644)
	run(t, clone, "git", "add", ".")
	run(t, clone, "git", "commit", "-m", "initial")
	run(t, clone, "git", "push", "origin", "HEAD")

	return clone
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
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
		t.Fatalf("%s %s failed: %s\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestIsGitRepo(t *testing.T) {
	repo := initTestRepo(t)

	if !IsGitRepo(repo) {
		t.Error("expected true for git repo")
	}

	notRepo := t.TempDir()
	if IsGitRepo(notRepo) {
		t.Error("expected false for non-repo dir")
	}
}

func TestBranchExists(t *testing.T) {
	repo := initTestRepo(t)

	// main/master should exist
	branch := currentBranch(t, repo)
	if !BranchExists(repo, branch) {
		t.Errorf("expected branch %q to exist", branch)
	}

	if BranchExists(repo, "nonexistent-branch") {
		t.Error("expected false for nonexistent branch")
	}
}

func TestCreateAndDeleteBranch(t *testing.T) {
	repo := initTestRepo(t)

	if err := CreateBranch(repo, "feat/test", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	if !BranchExists(repo, "feat/test") {
		t.Error("branch should exist after creation")
	}

	if err := DeleteBranch(repo, "feat/test", false); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if BranchExists(repo, "feat/test") {
		t.Error("branch should not exist after deletion")
	}
}

func TestCreateBranchWithStartPoint(t *testing.T) {
	repo := initTestRepo(t)
	branch := currentBranch(t, repo)

	if err := CreateBranch(repo, "feat/from-point", "origin/"+branch); err != nil {
		t.Fatalf("create with start point: %v", err)
	}

	if !BranchExists(repo, "feat/from-point") {
		t.Error("branch should exist")
	}
}

func TestWorktreeAddAndRemove(t *testing.T) {
	repo := initTestRepo(t)

	// Create a branch for the worktree
	CreateBranch(repo, "feat/wt-test", "")

	wtPath := filepath.Join(t.TempDir(), "worktree")

	if err := WorktreeAdd(repo, wtPath, "feat/wt-test"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Worktree dir should exist
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("worktree directory should exist")
	}

	// Should appear in list
	entries, err := WorktreeList(repo)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Branch == "feat/wt-test" {
			found = true
		}
	}
	if !found {
		t.Error("worktree should appear in list")
	}

	// Remove
	if err := WorktreeRemove(repo, wtPath); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestWorktreeHasBranch(t *testing.T) {
	repo := initTestRepo(t)

	CreateBranch(repo, "feat/check", "")
	wtPath := filepath.Join(t.TempDir(), "wt")
	WorktreeAdd(repo, wtPath, "feat/check")

	has, err := WorktreeHasBranch(repo, "feat/check")
	if err != nil {
		t.Fatalf("has branch: %v", err)
	}
	if !has {
		t.Error("expected branch to have a worktree")
	}

	has, err = WorktreeHasBranch(repo, "nonexistent")
	if err != nil {
		t.Fatalf("has branch: %v", err)
	}
	if has {
		t.Error("expected false for nonexistent branch")
	}

	// Cleanup
	WorktreeRemove(repo, wtPath)
}

func TestWorktreeListParsing(t *testing.T) {
	repo := initTestRepo(t)

	entries, err := WorktreeList(repo)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// Should have at least the main worktree
	if len(entries) < 1 {
		t.Error("expected at least 1 worktree entry")
	}

	// Main worktree should have a path
	if entries[0].Path == "" {
		t.Error("expected non-empty path for main worktree")
	}
}

func TestDefaultBranch(t *testing.T) {
	repo := initTestRepo(t)

	branch, err := DefaultBranch(repo)
	if err != nil {
		t.Fatalf("default branch: %v", err)
	}
	// Should be main or master
	if branch != "main" && branch != "master" {
		t.Errorf("expected 'main' or 'master', got %q", branch)
	}
}

func TestRepoStatus(t *testing.T) {
	repo := initTestRepo(t)

	// Clean repo
	status, err := RepoStatus(repo)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "" {
		t.Errorf("expected clean, got %q", status)
	}

	// Create a file to make it dirty
	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("dirty"), 0o644)

	status, err = RepoStatus(repo)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status == "" {
		t.Error("expected non-empty status for dirty repo")
	}
}

func TestCurrentBranch(t *testing.T) {
	repo := initTestRepo(t)

	branch, err := CurrentBranch(repo)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch == "" {
		t.Error("expected non-empty branch name")
	}
}

func TestFetch(t *testing.T) {
	repo := initTestRepo(t)

	// Should succeed (no-op on fresh clone)
	if err := Fetch(repo); err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestRebaseOnto(t *testing.T) {
	repo := initTestRepo(t)
	mainBranch := currentBranch(t, repo)

	// Create a feature branch
	CreateBranch(repo, "feat/rebase", "")
	wtPath := filepath.Join(t.TempDir(), "wt-rebase")
	WorktreeAdd(repo, wtPath, "feat/rebase")

	// Add a commit on main
	os.WriteFile(filepath.Join(repo, "main-change.txt"), []byte("main"), 0o644)
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "main commit")

	// Rebase feature onto main
	if err := RebaseOnto(wtPath, mainBranch); err != nil {
		t.Fatalf("rebase: %v", err)
	}

	// Cleanup
	WorktreeRemove(repo, wtPath)
}

func TestCommitsAheadBehind(t *testing.T) {
	repo := initTestRepo(t)
	mainBranch := currentBranch(t, repo)

	// Create feature branch with an extra commit
	CreateBranch(repo, "feat/count", "")
	wtPath := filepath.Join(t.TempDir(), "wt-count")
	WorktreeAdd(repo, wtPath, "feat/count")

	// Configure git user in worktree
	run(t, wtPath, "git", "config", "user.email", "test@test.com")
	run(t, wtPath, "git", "config", "user.name", "Test")

	os.WriteFile(filepath.Join(wtPath, "feature.txt"), []byte("feat"), 0o644)
	run(t, wtPath, "git", "add", ".")
	run(t, wtPath, "git", "commit", "-m", "feature commit")

	ahead, behind, err := CommitsAheadBehind(wtPath, mainBranch)
	if err != nil {
		t.Fatalf("ahead/behind: %v", err)
	}
	if ahead != 1 {
		t.Errorf("expected 1 ahead, got %d", ahead)
	}
	if behind != 0 {
		t.Errorf("expected 0 behind, got %d", behind)
	}

	// Cleanup
	WorktreeRemove(repo, wtPath)
}

func TestRemoteURL(t *testing.T) {
	repo := initTestRepo(t)

	url := RemoteURL(repo, "origin")
	if url == "" {
		t.Error("expected non-empty URL")
	}
}

func TestRemoteURLNonexistent(t *testing.T) {
	repo := initTestRepo(t)

	url := RemoteURL(repo, "nonexistent")
	if url != "" {
		t.Errorf("expected empty for nonexistent remote, got %q", url)
	}
}

func TestParseRemoteName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"git@github.com:owner/repo", "owner/repo"},
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"git@gitlab.com:org/group/project.git", "org/group/project"},
		{"", ""},
	}
	for _, tt := range tests {
		got := ParseRemoteName(tt.url)
		if got != tt.want {
			t.Errorf("ParseRemoteName(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestResolveBaseBranch(t *testing.T) {
	repo := initTestRepo(t)

	base, err := ResolveBaseBranch(repo)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.HasPrefix(base, "origin/") {
		t.Errorf("expected origin/ prefix, got %q", base)
	}
}

func TestRepoBaseBranchEmpty(t *testing.T) {
	repo := initTestRepo(t)
	// No .grove.toml — should return ""
	base := RepoBaseBranch(repo)
	if base != "" {
		t.Errorf("expected empty, got %q", base)
	}
}

func TestReadGroveConfig(t *testing.T) {
	ClearGroveConfigCache()
	repo := initTestRepo(t)

	// No .grove.toml — should return nil, nil
	cfg, err := ReadGroveConfig(repo)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil for missing .grove.toml")
	}

	// Clear cache and write a .grove.toml
	ClearGroveConfigCache()
	tomlContent := `base_branch = "stage"
setup = "npm install"
run = ["make build", "make serve"]
teardown = "make clean"
`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(tomlContent), 0o644)

	cfg, err = ReadGroveConfig(repo)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.BaseBranch != "stage" {
		t.Errorf("base_branch: got %q", cfg.BaseBranch)
	}
	if len(cfg.Setup) != 1 || cfg.Setup[0] != "npm install" {
		t.Errorf("setup: got %v", cfg.Setup)
	}
	if len(cfg.Run) != 2 || cfg.Run[0] != "make build" {
		t.Errorf("run: got %v", cfg.Run)
	}
	if cfg.Teardown != "make clean" {
		t.Errorf("teardown: got %q", cfg.Teardown)
	}
}

func TestGitError(t *testing.T) {
	err := &GitError{Cmd: "status", Stderr: "not a repo"}
	if !strings.Contains(err.Error(), "status") {
		t.Error("error should contain command")
	}
	if !strings.Contains(err.Error(), "not a repo") {
		t.Error("error should contain stderr")
	}
}

func TestWorktreeRepair(t *testing.T) {
	repo := initTestRepo(t)

	CreateBranch(repo, "feat/repair", "")
	wtPath := filepath.Join(t.TempDir(), "wt-repair")
	WorktreeAdd(repo, wtPath, "feat/repair")

	// Repair should succeed (no-op when nothing is broken)
	if err := WorktreeRepair(repo, wtPath); err != nil {
		t.Fatalf("repair: %v", err)
	}

	WorktreeRemove(repo, wtPath)
}

func TestDeleteBranchForce(t *testing.T) {
	repo := initTestRepo(t)

	CreateBranch(repo, "feat/force-del", "")

	// Force delete with -D
	if err := DeleteBranch(repo, "feat/force-del", true); err != nil {
		t.Fatalf("force delete: %v", err)
	}

	if BranchExists(repo, "feat/force-del") {
		t.Error("branch should be deleted")
	}
}

func TestPRStatusNoGH(t *testing.T) {
	repo := initTestRepo(t)

	// If gh isn't available or no PR exists, should return nil
	result := PRStatus(repo)
	// Either nil (no gh) or nil (no PR) — both are fine
	if result != nil {
		t.Logf("PR status returned: %v (gh must be available)", result)
	}
}

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://github.com/owner/repo.git", true},
		{"http://github.com/owner/repo.git", true},
		{"https://github.com/owner/repo", true},
		{"git@github.com:owner/repo.git", true},
		{"git@gitlab.com:org/project.git", true},
		{"my-repo", false},
		{"some-local-name", false},
		{"", false},
		{"file:///tmp/repos/my-repo.git", true},
		{"https://", true}, // degenerate but still a URL
	}
	for _, tt := range tests {
		got := IsGitURL(tt.input)
		if got != tt.want {
			t.Errorf("IsGitURL(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/owner/my-repo.git", "my-repo"},
		{"https://github.com/owner/my-repo", "my-repo"},
		{"git@github.com:owner/my-repo.git", "my-repo"},
		{"git@github.com:owner/my-repo", "my-repo"},
		{"https://github.com/owner/my-repo/", "my-repo"},
		{"https://github.com/owner/my-repo.git/", "my-repo"},
		{"file:///tmp/repos/remote-origin.git", "remote-origin"},
		{"", ""},
	}
	for _, tt := range tests {
		got := RepoNameFromURL(tt.url)
		if got != tt.want {
			t.Errorf("RepoNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestClone(t *testing.T) {
	// Create a bare repo to serve as the "remote"
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	run(t, dir, "git", "init", "--bare", bare)

	// Seed it with a commit via a temp clone
	tmp := filepath.Join(dir, "tmp")
	run(t, dir, "git", "clone", bare, tmp)
	run(t, tmp, "git", "config", "user.email", "test@test.com")
	run(t, tmp, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(tmp, "README.md"), []byte("# test"), 0o644)
	run(t, tmp, "git", "add", ".")
	run(t, tmp, "git", "commit", "-m", "initial")
	run(t, tmp, "git", "push", "origin", "HEAD")

	// Clone using our function (bare path acts as a local URL)
	destDir := filepath.Join(dir, "repos")
	os.MkdirAll(destDir, 0o755)

	clonedPath, name, err := Clone(bare, destDir)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if !IsGitRepo(clonedPath) {
		t.Error("cloned path should be a git repo")
	}
	if name != "origin" {
		t.Errorf("expected name %q, got %q", "origin", name)
	}

	// Clone again — should succeed (repo already exists)
	clonedPath2, _, err := Clone(bare, destDir)
	if err != nil {
		t.Fatalf("Clone (idempotent): %v", err)
	}
	if clonedPath != clonedPath2 {
		t.Errorf("expected same path, got %q vs %q", clonedPath, clonedPath2)
	}
}

func TestCloneExistingNonRepo(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, "repos")
	os.MkdirAll(destDir, 0o755)

	// Create a non-repo directory that would conflict
	os.MkdirAll(filepath.Join(destDir, "origin.git"), 0o755)

	_, _, err := Clone(filepath.Join(dir, "origin.git"), destDir)
	if err == nil {
		t.Error("expected error when destination exists but is not a git repo")
	}
}

func TestClonePathTraversal(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Clone("file:///evil/..", dir)
	if err == nil {
		t.Error("expected error for path traversal URL")
	}
	if !strings.Contains(err.Error(), "unsafe") {
		t.Errorf("expected 'unsafe' in error, got: %v", err)
	}
}

func TestCloneRetryCleanup(t *testing.T) {
	// Speed up retries for testing
	origBackoff := cloneBackoff
	cloneBackoff = 1 * time.Millisecond
	t.Cleanup(func() { cloneBackoff = origBackoff })

	destDir := t.TempDir()

	_, _, err := Clone("file:///nonexistent/repo.git", destDir)
	if err == nil {
		t.Fatal("expected error cloning nonexistent repo")
	}

	// Verify no partial directory remains after exhausted retries
	partial := filepath.Join(destDir, "repo")
	if _, statErr := os.Stat(partial); !os.IsNotExist(statErr) {
		t.Error("partial clone directory should be cleaned up after retries")
	}

	// Verify the error message is propagated
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should reference the URL, got: %v", err)
	}
}

// helper
func currentBranch(t *testing.T, repo string) string {
	t.Helper()
	return run(t, repo, "git", "branch", "--show-current")
}
