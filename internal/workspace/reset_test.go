package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/gitops"
)

func TestResetNotFound(t *testing.T) {
	env := setupTestEnv(t)

	err := env.svc.Reset("nonexistent", false)
	if err == nil {
		t.Error("expected error")
	}
}

func TestResetAlreadyOnBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createWorkspace("reset-home", "feat/reset-home", []string{"api"})

	if err := env.svc.Reset("reset-home", false); err != nil {
		t.Fatalf("reset: %v", err)
	}

	wt := filepath.Join(env.wsDir, "reset-home", "api")
	branch, err := gitops.CurrentBranch(wt)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch != "feat/reset-home" {
		t.Errorf("branch: got %q, want feat/reset-home", branch)
	}
}

func TestResetSwitchesAndSyncs(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")
	env.createWorkspace("reset-ws", "feat/reset", []string{"api"})
	wt := filepath.Join(env.wsDir, "reset-ws", "api")

	env.run(wt, "git", "switch", "-q", "-c", "feat/wander")

	os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("new"), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "upstream change")
	env.run(repo, "git", "push", "-q", "origin", "HEAD")

	if err := env.svc.Reset("reset-ws", false); err != nil {
		t.Fatalf("reset: %v", err)
	}

	branch, err := gitops.CurrentBranch(wt)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch != "feat/reset" {
		t.Errorf("branch: got %q, want feat/reset", branch)
	}
	if _, err := os.Stat(filepath.Join(wt, "upstream.txt")); os.IsNotExist(err) {
		t.Error("upstream change should be rebased into worktree")
	}
}

func TestResetSkipsDirtyWanderer(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createWorkspace("reset-dirty", "feat/reset-dirty", []string{"api"})
	wt := filepath.Join(env.wsDir, "reset-dirty", "api")

	env.run(wt, "git", "switch", "-q", "-c", "feat/wander")
	os.WriteFile(filepath.Join(wt, "dirt.txt"), []byte("uncommitted"), 0o644)

	if err := env.svc.Reset("reset-dirty", false); err != nil {
		t.Fatalf("reset: %v", err)
	}

	branch, err := gitops.CurrentBranch(wt)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch != "feat/wander" {
		t.Errorf("branch: got %q, want feat/wander (dirty skip)", branch)
	}
}

func TestResetDiscardSwitchesDirtyWanderer(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createWorkspace("reset-discard", "feat/reset-discard", []string{"api"})
	wt := filepath.Join(env.wsDir, "reset-discard", "api")

	env.run(wt, "git", "switch", "-q", "-c", "feat/wander")
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("dirty"), 0o644)

	if err := env.svc.Reset("reset-discard", true); err != nil {
		t.Fatalf("reset: %v", err)
	}

	branch, err := gitops.CurrentBranch(wt)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch != "feat/reset-discard" {
		t.Errorf("branch: got %q, want feat/reset-discard", branch)
	}
	data, err := os.ReadFile(filepath.Join(wt, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if string(data) == "dirty" {
		t.Error("tracked changes should be discarded")
	}
}

func TestResetMixedRepos(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createRepoWithRemote("web")
	env.createWorkspace("reset-mixed", "feat/reset-mixed", []string{"api", "web"})

	api := filepath.Join(env.wsDir, "reset-mixed", "api")
	web := filepath.Join(env.wsDir, "reset-mixed", "web")
	env.run(api, "git", "switch", "-q", "-c", "feat/wander")

	if err := env.svc.Reset("reset-mixed", false); err != nil {
		t.Fatalf("reset: %v", err)
	}

	for _, wt := range []string{api, web} {
		branch, err := gitops.CurrentBranch(wt)
		if err != nil {
			t.Fatalf("current branch %s: %v", wt, err)
		}
		if branch != "feat/reset-mixed" {
			t.Errorf("%s branch: got %q, want feat/reset-mixed", wt, branch)
		}
	}
}

func TestResetDetachedHead(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.createWorkspace("reset-detach", "feat/reset-detach", []string{"api"})
	wt := filepath.Join(env.wsDir, "reset-detach", "api")

	env.run(wt, "git", "switch", "-q", "--detach")

	if err := env.svc.Reset("reset-detach", false); err != nil {
		t.Fatalf("reset: %v", err)
	}

	branch, err := gitops.CurrentBranch(wt)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch != "feat/reset-detach" {
		t.Errorf("branch: got %q, want feat/reset-detach", branch)
	}
}
