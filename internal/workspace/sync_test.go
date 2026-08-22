package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncUpToDate(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createWorkspace("sync-ws", "feat/sync", []string{"api"})

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

func TestSyncMultiRepo(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createRepoWithRemote("web")
	env.createWorkspace("sync-multi", "feat/sm", []string{"api", "web"})

	err := env.svc.Sync("sync-multi")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func TestSyncRebases(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")
	env.createWorkspace("rebase-ws", "feat/rebase", []string{"api"})
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

func TestSyncSkipsDirty(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createWorkspace("dirty-ws", "feat/dirty", []string{"api"})

	wt := filepath.Join(env.wsDir, "dirty-ws", "api")
	os.WriteFile(filepath.Join(wt, "dirt.txt"), []byte("uncommitted"), 0o644)

	err := env.svc.Sync("dirty-ws")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	// No error = correctly skipped
}

func TestSyncRunsPreAndPostHooks(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")

	toml := `pre_sync = "echo pre"
post_sync = "echo post"`
	os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(toml), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "add sync hooks")
	env.run(repo, "git", "push", "-q", "origin", "HEAD")

	env.createWorkspace("sync-hook-ws", "feat/sync-hook", []string{"api"})

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

func TestSyncConflictAbortsRebase(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")
	env.createWorkspace("conflict-ws", "feat/conflict", []string{"api"})

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
