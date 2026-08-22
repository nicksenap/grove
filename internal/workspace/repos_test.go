package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/gitops"
)

func TestAddReposSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	env.createWorkspace("add-ws", "feat/add", []string{"api"})

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
	env.createWorkspace("dup-ws", "feat/dup", []string{"api"})

	// Adding same repo again should be a no-op without wait progress.
	output := captureStderr(t, func() {
		if err := env.svc.AddRepos("dup-ws", []string{"api"}, env.repoMap); err != nil {
			t.Fatalf("add duplicate: %v", err)
		}
	})
	if strings.Contains(output, "Adding") {
		t.Errorf("no-op addition should not show wait progress, got: %q", output)
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

func TestAddReposRollsBackEarlierAdditions(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	worker := env.createRepo("worker")
	if err := env.createWorkspace("add-rollback", "feat/add-rollback", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	conflictPath := filepath.Join(env.dir, "worker-conflict")
	env.run(worker, "git", "worktree", "add", "-q", "-b", "feat/add-rollback", conflictPath)

	err := env.svc.AddRepos("add-rollback", []string{"web", "worker"}, env.repoMap)
	if err == nil || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("expected worker conflict, got %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("add-rollback")
	if ws.FindRepo("web") != nil {
		t.Fatal("rolled-back web repo should not be saved")
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "web")); !os.IsNotExist(err) {
		t.Error("rolled-back web worktree should be removed")
	}
	if gitops.BranchExists(env.repoMap["web"], "feat/add-rollback") {
		t.Error("branch created for rolled-back web repo should be removed")
	}
}

func TestAddReposRollbackPreservesPreexistingBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	web := env.createRepo("web")
	worker := env.createRepo("worker")
	if err := env.createWorkspace("add-preserve", "feat/add-preserve", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	env.run(web, "git", "branch", "feat/add-preserve")
	conflictPath := filepath.Join(env.dir, "worker-preserve-conflict")
	env.run(worker, "git", "worktree", "add", "-q", "-b", "feat/add-preserve", conflictPath)

	err := env.svc.AddRepos("add-preserve", []string{"web", "worker"}, env.repoMap)
	if err == nil {
		t.Fatal("expected worker conflict")
	}
	if _, err := os.Stat(filepath.Join(env.wsDir, "add-preserve", "web")); !os.IsNotExist(err) {
		t.Error("rolled-back web worktree should be removed")
	}
	if !gitops.BranchExists(web, "feat/add-preserve") {
		t.Error("rollback should preserve a branch it did not create")
	}
}

func TestAddReposCleansBranchWhenWorktreeAddFails(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	web := env.createRepo("web")
	if err := env.createWorkspace("add-provision-fail", "feat/add-provision-fail", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	blockedPath := filepath.Join(env.wsDir, "add-provision-fail", "web")
	if err := os.MkdirAll(blockedPath, 0o755); err != nil {
		t.Fatalf("create blocked path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blockedPath, "keep.txt"), []byte("occupied"), 0o644); err != nil {
		t.Fatalf("write blocked path: %v", err)
	}

	if err := env.svc.AddRepos("add-provision-fail", []string{"web"}, env.repoMap); err == nil {
		t.Fatal("expected worktree add failure")
	}
	if gitops.BranchExists(web, "feat/add-provision-fail") {
		t.Error("branch created before failed worktree add should be cleaned up")
	}
	if _, err := os.Stat(filepath.Join(blockedPath, "keep.txt")); err != nil {
		t.Fatal("pre-existing blocked path should remain untouched")
	}
}

func TestRemoveReposSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createWorkspace("rm-ws", "feat/rm", []string{"api", "web"})

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
	env.createWorkspace("rm-multi", "feat/rm-multi", []string{"api", "web", "worker"})

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
	env.createWorkspace("rm-ne", "feat/rm-ne", []string{"api"})

	// Removing a repo not in workspace should be a silent no-op.
	output := captureStderr(t, func() {
		if err := env.svc.RemoveRepos("rm-ne", []string{"nonexistent"}); err != nil {
			t.Fatalf("remove nonexistent: %v", err)
		}
	})
	if output != "" {
		t.Errorf("no-op removal should be silent, got: %q", output)
	}
}

func TestRemoveReposRejectsDirtyWorktreeByDefault(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	if err := env.createWorkspace("dirty-remove", "feat/dirty-remove", []string{"api", "web"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("dirty-remove")
	web := ws.FindRepo("web")
	if err := os.WriteFile(filepath.Join(web.WorktreePath, "dirty.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	err := env.svc.RemoveRepos("dirty-remove", []string{"web"})
	if err == nil || !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("expected repo-named dirty error, got %v", err)
	}
	if saved, _ := env.svc.State.GetWorkspace("dirty-remove"); saved == nil || saved.FindRepo("web") == nil {
		t.Fatal("dirty repo should remain in state")
	}
}

func TestRemoveReposRetainsOnlyFailedEntries(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createRepo("worker")
	if err := env.createWorkspace("partial-remove", "feat/partial-remove", []string{"api", "web", "worker"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("partial-remove")
	apiPath := ws.FindRepo("api").WorktreePath
	webPath := ws.FindRepo("web").WorktreePath
	env.svc.RemoveWorktree = func(repo, path string, force bool) error {
		if path == webPath {
			return fmt.Errorf("simulated git failure")
		}
		return gitops.WorktreeRemove(repo, path, force)
	}

	err := env.svc.RemoveRepos("partial-remove", []string{"api", "web"})
	if err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("expected web removal error, got %v", err)
	}
	if _, err := os.Stat(apiPath); !os.IsNotExist(err) {
		t.Error("successfully removed api worktree should be gone")
	}
	if _, err := os.Stat(webPath); err != nil {
		t.Fatalf("failed web worktree should remain: %v", err)
	}

	saved, _ := env.svc.State.GetWorkspace("partial-remove")
	if saved == nil || saved.FindRepo("api") != nil || saved.FindRepo("web") == nil || saved.FindRepo("worker") == nil {
		t.Fatalf("state should retain failed and untouched repos: %+v", saved)
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

	env.createWorkspace("td-rm-ws", "feat/td-rm", []string{"api", "web"})
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

func TestAddReposBranchConflict(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	// Create first workspace, consuming the branch on "web"
	env.createWorkspace("ws1", "feat/conflict", []string{"web"})

	// Create second workspace with "api" only
	env.createWorkspace("ws2", "feat/other", []string{"api"})

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

	env.createWorkspace("setup-add-ws", "feat/setup-add", []string{"api"})

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

func TestRemoveReposWorkspaceNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := env.svc.RemoveRepos("nonexistent", []string{"api"})
	if err == nil {
		t.Error("expected error for nonexistent workspace")
	}
}

func TestAddReposShowsProgressOnStderr(t *testing.T) {
	tests := []struct {
		name     string
		toAdd    []string
		expected string
	}{
		{name: "singular", toAdd: []string{"web"}, expected: "Adding 1 repo to add-progress-singular. Please wait."},
		{name: "plural", toAdd: []string{"web", "worker"}, expected: "Adding 2 repos to add-progress-plural. Please wait."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupTestEnv(t)
			env.createRepo("api")
			env.createRepo("web")
			env.createRepo("worker")

			wsName := "add-progress-" + tt.name
			if err := env.createWorkspace(wsName, "feat/"+wsName, []string{"api"}); err != nil {
				t.Fatalf("create: %v", err)
			}

			output := captureStderr(t, func() {
				if err := env.svc.AddRepos(wsName, tt.toAdd, env.repoMap); err != nil {
					t.Fatalf("add repos: %v", err)
				}
			})
			if !strings.Contains(output, tt.expected) {
				t.Errorf("expected %q in stderr, got: %q", tt.expected, output)
			}
		})
	}
}

func TestRemoveReposShowsProgressOnStderr(t *testing.T) {
	tests := []struct {
		name     string
		toRemove []string
		expected string
	}{
		{name: "singular", toRemove: []string{"web"}, expected: "Removing 1 repo from remove-progress-singular. Please wait."},
		{name: "plural", toRemove: []string{"web", "worker"}, expected: "Removing 2 repos from remove-progress-plural. Please wait."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupTestEnv(t)
			env.createRepo("api")
			env.createRepo("web")
			env.createRepo("worker")

			wsName := "remove-progress-" + tt.name
			if err := env.createWorkspace(wsName, "feat/"+wsName, []string{"api", "web", "worker"}); err != nil {
				t.Fatalf("create: %v", err)
			}

			output := captureStderr(t, func() {
				if err := env.svc.RemoveRepos(wsName, tt.toRemove); err != nil {
					t.Fatalf("remove repos: %v", err)
				}
			})
			if !strings.Contains(output, tt.expected) {
				t.Errorf("expected %q in stderr, got: %q", tt.expected, output)
			}
		})
	}
}
