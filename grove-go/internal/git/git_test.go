package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo creates a temporary git repo with an initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}
	return dir
}

func TestIsGitRepo(t *testing.T) {
	repo := initTestRepo(t)
	if !IsGitRepo(repo) {
		t.Error("expected true for git repo")
	}
	if IsGitRepo(t.TempDir()) {
		t.Error("expected false for non-repo")
	}
}

func TestCurrentBranch(t *testing.T) {
	repo := initTestRepo(t)
	branch, err := CurrentBranch(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Default branch is either main or master depending on git config
	if branch != "main" && branch != "master" {
		t.Errorf("branch = %q, want main or master", branch)
	}
}

func TestBranchExists(t *testing.T) {
	repo := initTestRepo(t)
	branch, _ := CurrentBranch(repo)

	if !BranchExists(repo, branch) {
		t.Errorf("expected %q to exist", branch)
	}
	if BranchExists(repo, "nonexistent-branch-xyz") {
		t.Error("expected nonexistent branch to not exist")
	}
}

func TestCreateBranch(t *testing.T) {
	repo := initTestRepo(t)

	if err := CreateBranch(repo, "test-branch", ""); err != nil {
		t.Fatal(err)
	}
	if !BranchExists(repo, "test-branch") {
		t.Error("expected test-branch to exist after creation")
	}
}

func TestWorktreeAddRemove(t *testing.T) {
	repo := initTestRepo(t)

	// Create branch for worktree
	if err := CreateBranch(repo, "wt-branch", ""); err != nil {
		t.Fatal(err)
	}

	wtPath := filepath.Join(t.TempDir(), "worktree")
	if err := WorktreeAdd(repo, wtPath, "wt-branch"); err != nil {
		t.Fatal(err)
	}

	// Verify worktree exists
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("worktree directory should exist")
	}

	// Verify worktree is listed
	entries, err := WorktreeList(repo)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Branch == "wt-branch" {
			found = true
			break
		}
	}
	if !found {
		t.Error("worktree not found in list")
	}

	// Remove
	if err := WorktreeRemove(repo, wtPath); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeHasBranch(t *testing.T) {
	repo := initTestRepo(t)
	branch, _ := CurrentBranch(repo)

	// The main worktree uses the current branch
	if !WorktreeHasBranch(repo, branch) {
		t.Errorf("expected %q to be in use by a worktree", branch)
	}
	if WorktreeHasBranch(repo, "nonexistent") {
		t.Error("expected nonexistent branch to not be in use")
	}
}

func TestParseRemoteName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"git@github.com:org/my-repo.git", "org/my-repo"},
		{"invalid", ""},
	}
	for _, tt := range tests {
		got := ParseRemoteName(tt.url)
		if got != tt.want {
			t.Errorf("ParseRemoteName(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestRepoStatus(t *testing.T) {
	repo := initTestRepo(t)
	status, err := RepoStatus(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Clean repo should have empty status
	if status != "" {
		t.Errorf("expected empty status for clean repo, got %q", status)
	}

	// Create an untracked file
	os.WriteFile(filepath.Join(repo, "test.txt"), []byte("hello"), 0o644)
	status, err = RepoStatus(repo)
	if err != nil {
		t.Fatal(err)
	}
	if status == "" {
		t.Error("expected non-empty status with untracked file")
	}
}

func TestReadGroveConfig(t *testing.T) {
	dir := t.TempDir()

	// No .grove.toml should return nil
	cfg := ReadGroveConfig(dir)
	if cfg != nil {
		t.Error("expected nil for repo without .grove.toml")
	}

	// Write a .grove.toml
	content := `base_branch = "develop"
setup = "npm install"
run = ["npm start", "npm run watch"]
`
	os.WriteFile(filepath.Join(dir, ".grove.toml"), []byte(content), 0o644)

	// Clear cache
	groveConfigCache.Delete(filepath.Join(dir, ".grove.toml"))

	cfg = ReadGroveConfig(dir)
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want %q", cfg.BaseBranch, "develop")
	}
	if len(cfg.Setup) != 1 || cfg.Setup[0] != "npm install" {
		t.Errorf("Setup = %v, want [npm install]", cfg.Setup)
	}
	if len(cfg.Run) != 2 {
		t.Errorf("Run = %v, want 2 entries", cfg.Run)
	}
}

func TestRepoHookCommands(t *testing.T) {
	dir := t.TempDir()
	content := `setup = "make build"
teardown = "make clean"
`
	os.WriteFile(filepath.Join(dir, ".grove.toml"), []byte(content), 0o644)

	cmds := RepoHookCommands(dir, "setup")
	if len(cmds) != 1 || cmds[0] != "make build" {
		t.Errorf("setup = %v", cmds)
	}
	cmds = RepoHookCommands(dir, "teardown")
	if len(cmds) != 1 || cmds[0] != "make clean" {
		t.Errorf("teardown = %v", cmds)
	}
	cmds = RepoHookCommands(dir, "run")
	if len(cmds) != 0 {
		t.Errorf("run = %v, want empty", cmds)
	}
}

func TestDefaultBranch(t *testing.T) {
	repo := initTestRepo(t)
	branch := DefaultBranch(repo)
	// Should detect the initial branch
	if branch != "main" && branch != "master" {
		t.Errorf("DefaultBranch = %q", branch)
	}
}

func TestGitError(t *testing.T) {
	err := &Error{Cmd: "status", Stderr: "not a repo"}
	if err.Error() != "git status: not a repo" {
		t.Errorf("Error() = %q", err.Error())
	}
}
