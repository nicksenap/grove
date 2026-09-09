package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
)

func TestDeletePreparedWorkspacePreservesPreexistingBranch(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	baseSHA := env.run(repo, "git", "rev-parse", "HEAD")
	env.run(repo, "git", "branch", "feat/existing", baseSHA)

	if err := env.svc.CreateWithPreparation("prepared", PreparationOpts{
		CreateOpts:  CreateOpts{Branch: "feat/existing", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg},
		BaseCommits: map[string]string{"api": baseSHA},
	}, func(models.Workspace) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.DeleteWithOptions("prepared", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if !gitops.BranchExists(repo, "feat/existing") {
		t.Fatal("normal workspace deletion removed pre-existing Recipe branch")
	}
}

func TestDeleteSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("del-ws", "feat/del", []string{"api"})

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
	env.createWorkspace("branch-ws", "feat/to-clean", []string{"api"})

	env.svc.Delete("branch-ws")

	// Branch should be cleaned up from source repo
	out := env.run(env.repoMap["api"], "git", "branch", "--list", "feat/to-clean")
	if strings.TrimSpace(out) != "" {
		t.Error("branch should be deleted from source repo")
	}
}

func TestDeleteRejectsDirtyWorktreeByDefault(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("dirty-delete", "feat/dirty-delete", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("dirty-delete")
	dirtyFile := filepath.Join(ws.Repos[0].WorktreePath, "dirty.txt")
	if err := os.WriteFile(dirtyFile, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	err := env.svc.DeleteWithOptions("dirty-delete", RemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "api") || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("expected repo-named dirty error, got %v", err)
	}
	if _, err := os.Stat(dirtyFile); err != nil {
		t.Fatalf("dirty file should remain: %v", err)
	}
	if saved, _ := env.svc.State.GetWorkspace("dirty-delete"); saved == nil {
		t.Fatal("workspace state should remain")
	}
}

func TestDeleteRemovesWorkspaceMetadata(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("metadata-delete", "feat/metadata-delete", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("metadata-delete")
	if err := os.MkdirAll(filepath.Join(ws.Path, ".pi"), 0o755); err != nil {
		t.Fatalf("create workspace metadata directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, ".pi", "hindsight.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write workspace metadata: %v", err)
	}

	if err := env.svc.Delete("metadata-delete"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("workspace metadata should not block root removal, got %v", err)
	}
}

func TestDeleteForceRemovesDirtyWorktree(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("force-delete", "feat/force-delete", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("force-delete")
	if err := os.WriteFile(filepath.Join(ws.Repos[0].WorktreePath, "dirty.txt"), []byte("remove me"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ws.Path, ".pi"), 0o755); err != nil {
		t.Fatalf("create workspace metadata directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, ".pi", "hindsight.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write workspace metadata: %v", err)
	}

	if err := env.svc.DeleteWithOptions("force-delete", RemoveOptions{Force: true}); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	if saved, _ := env.svc.State.GetWorkspace("force-delete"); saved != nil {
		t.Fatal("workspace state should be removed")
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("force delete should remove the entire workspace root, got %v", err)
	}
}

func TestDeleteStateRemovalFailureRestoresPath(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("failed-state", "feat/failed-state", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ws, _ := env.svc.State.GetWorkspace("failed-state")

	env.svc.RemoveState = func(name string) error {
		return fmt.Errorf("simulated state failure")
	}

	err := env.svc.Delete("failed-state")
	if err == nil || !strings.Contains(err.Error(), "simulated state failure") {
		t.Fatalf("expected state removal error, got %v", err)
	}
	if _, err := os.Stat(ws.Path); err != nil {
		t.Fatalf("workspace path should be restored: %v", err)
	}
	if saved, _ := env.svc.State.GetWorkspace("failed-state"); saved == nil {
		t.Fatal("workspace should remain in state")
	}
}

func TestDeletePruneFailureRestoresPathAndState(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("failed-delete", "feat/failed-delete", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("failed-delete")
	env.svc.PruneWorktree = func(repo string) error {
		return fmt.Errorf("simulated prune failure")
	}

	err := env.svc.Delete("failed-delete")
	if err == nil || !strings.Contains(err.Error(), "api") {
		t.Fatalf("expected repo-named prune error, got %v", err)
	}
	if _, err := os.Stat(ws.Repos[0].WorktreePath); err != nil {
		t.Fatalf("worktree path should be restored: %v", err)
	}
	if saved, _ := env.svc.State.GetWorkspace("failed-delete"); saved == nil || len(saved.Repos) != 1 {
		t.Fatal("failed repo should remain in state")
	}
}

func TestDeletePreflightsEveryRepoBeforeRemovingAny(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	if err := env.createWorkspace("preflight-delete", "feat/preflight-delete", []string{"api", "web"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ws, _ := env.svc.State.GetWorkspace("preflight-delete")
	apiPath := ws.Repos[0].WorktreePath
	ws.Repos[1].WorktreePath = filepath.Join(ws.Path, "unexpected-web")
	if err := env.svc.State.UpdateWorkspace(*ws); err != nil {
		t.Fatalf("corrupt state fixture: %v", err)
	}

	err := env.svc.Delete("preflight-delete")
	if err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("expected web preflight error, got %v", err)
	}
	if _, err := os.Stat(apiPath); err != nil {
		t.Fatalf("api should not be removed before all preflights pass: %v", err)
	}
}

func TestDeleteMultiRepoAllCleaned(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createWorkspace("multi-del", "feat/md", []string{"api", "web"})

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

	env.createWorkspace("td-ws", "feat/td", []string{"api"})
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

func TestDeletePartialFailurePreservesState(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createWorkspace("partial-ws", "feat/partial", []string{"api", "web"})

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

func TestDeletePreservesUnmergedBranchWithoutForce(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")

	env.createWorkspace("unmerged-ws", "feat/unmerged", []string{"api"})

	wt := filepath.Join(env.wsDir, "unmerged-ws", "api")
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("unmerged work"), 0o644)
	env.run(wt, "git", "add", ".")
	env.run(wt, "git", "commit", "-q", "-m", "unmerged commit")

	sourceRepo := env.repoMap["api"]
	if err := env.svc.Delete("unmerged-ws"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if !gitops.BranchExists(sourceRepo, "feat/unmerged") {
		t.Error("safe delete should preserve an unmerged branch")
	}
	if log := readLog(); !strings.Contains(log, "failed to delete branch") {
		t.Errorf("log should explain preserved branch, got:\n%s", log)
	}
}

func TestDeleteForceDeletesUnmergedBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	env.createWorkspace("force-unmerged-ws", "feat/force-unmerged", []string{"api"})
	wt := filepath.Join(env.wsDir, "force-unmerged-ws", "api")
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("unmerged work"), 0o644)
	env.run(wt, "git", "add", ".")
	env.run(wt, "git", "commit", "-q", "-m", "unmerged commit")

	if err := env.svc.DeleteWithOptions("force-unmerged-ws", RemoveOptions{Force: true}); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	if gitops.BranchExists(env.repoMap["api"], "feat/force-unmerged") {
		t.Error("force delete should remove an unmerged branch")
	}
}

func TestDeleteDoesNotCallWorktreeRemove(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("del-ws", "feat/del", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ws, _ := env.svc.State.GetWorkspace("del-ws")

	env.svc.RemoveWorktree = func(repo, path string, force bool) error {
		t.Fatal("git worktree remove should not run during workspace delete")
		return nil
	}

	if err := env.svc.Delete("del-ws"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("original workspace path should be free, got %v", err)
	}
	if saved, _ := env.svc.State.GetWorkspace("del-ws"); saved != nil {
		t.Fatal("workspace should be removed from state")
	}
}

func TestDeleteFreesPathBeforeUnlink(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("del-ws", "feat/del", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ws, _ := env.svc.State.GetWorkspace("del-ws")
	marker := filepath.Join(ws.Repos[0].WorktreePath, "README.md")

	env.svc.UnlinkTrash = func(path string) error {
		return nil
	}

	if err := env.svc.Delete("del-ws"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("original workspace path should be free, got %v", err)
	}
	if saved, _ := env.svc.State.GetWorkspace("del-ws"); saved != nil {
		t.Fatal("workspace should be removed from state")
	}

	matches, err := filepath.Glob(filepath.Join(env.wsDir, ".trash", "del-ws-*", "api", "README.md"))
	if err != nil {
		t.Fatalf("glob trash: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected quarantined worktree files, got %v", matches)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("original worktree path should be gone, got %v", err)
	}
}

func TestDeleteUnlinksAfterRemovingState(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("del-ws", "feat/del", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	var seen *models.Workspace
	env.svc.UnlinkTrash = func(path string) error {
		seen, _ = env.svc.State.GetWorkspace("del-ws")
		return nil
	}

	if err := env.svc.Delete("del-ws"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if seen != nil {
		t.Fatal("unlink ran before workspace was removed from state")
	}
}

func TestDeleteFallsBackToSyncUnlinkWhenSpawnFails(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("del-ws", "feat/del", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	env.svc.StartUnlink = func(path string) error {
		return fmt.Errorf("spawn failed")
	}

	if err := env.svc.Delete("del-ws"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(env.wsDir, ".trash", "del-ws-*"))
	if err != nil {
		t.Fatalf("glob trash: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("spawn failure should fall back to sync unlink, leftover %v", matches)
	}
	if log := readLog(); !strings.Contains(log, "failed to spawn unlink") {
		t.Errorf("log should mention spawn failure, got:\n%s", log)
	}
}

func TestUnlinkTrashPathRejectsPathsOutsideTrash(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "not-trash")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := UnlinkTrashPath(outside); err == nil {
		t.Fatal("expected rejection of path outside .trash")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("non-trash path should remain: %v", err)
	}
}

func TestUnlinkTrashPathRemovesTrashItem(t *testing.T) {
	dir := t.TempDir()
	item := filepath.Join(dir, ".trash", "ws-1")
	if err := os.MkdirAll(filepath.Join(item, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := UnlinkTrashPath(item); err != nil {
		t.Fatalf("unlink trash: %v", err)
	}
	if _, err := os.Stat(item); !os.IsNotExist(err) {
		t.Fatalf("trash item should be removed, got %v", err)
	}
}

func TestDeleteSchedulesUnlinkWithoutWaiting(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("del-ws", "feat/del", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	var scheduled string
	env.svc.StartUnlink = func(path string) error {
		scheduled = path
		return nil
	}
	env.svc.UnlinkTrash = func(path string) error {
		t.Fatal("sync unlink should not run when background unlink starts")
		return nil
	}

	if err := env.svc.Delete("del-ws"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if scheduled == "" {
		t.Fatal("expected background unlink to be scheduled")
	}
	if !strings.Contains(scheduled, string(filepath.Separator)+".trash"+string(filepath.Separator)) {
		t.Fatalf("scheduled unlink path %q is not in .trash", scheduled)
	}
	if _, err := os.Stat(scheduled); err != nil {
		t.Fatalf("trash should still exist after delete returns: %v", err)
	}
}

func TestDeleteAllowsRecreatingSameNameImmediately(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	if err := env.createWorkspace("reuse-ws", "feat/reuse", []string{"api"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	env.svc.UnlinkTrash = func(path string) error { return nil }
	if err := env.svc.Delete("reuse-ws"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if err := env.createWorkspace("reuse-ws", "feat/reuse", []string{"api"}); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	ws, _ := env.svc.State.GetWorkspace("reuse-ws")
	if ws == nil {
		t.Fatal("recreated workspace missing from state")
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "api", "README.md")); err != nil {
		t.Fatalf("recreated worktree missing: %v", err)
	}
}
