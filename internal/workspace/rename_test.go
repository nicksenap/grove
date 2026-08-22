package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("old-name", "feat/rename", []string{"api"})

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
	env.createWorkspace("ws-a", "feat/a", []string{"api"})
	env.createWorkspace("ws-b", "feat/b", []string{"web"})

	err := env.svc.Rename("ws-a", "ws-b")
	if err == nil {
		t.Error("expected error for taken name")
	}
}

func TestRenamePreservesCreatedAt(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("preserve-ws", "feat/preserve", []string{"api"})

	ws, _ := env.svc.State.GetWorkspace("preserve-ws")
	originalCreatedAt := ws.CreatedAt

	env.svc.Rename("preserve-ws", "renamed-ws")

	ws, _ = env.svc.State.GetWorkspace("renamed-ws")
	if ws.CreatedAt != originalCreatedAt {
		t.Errorf("created_at changed: %q -> %q", originalCreatedAt, ws.CreatedAt)
	}
}

func TestReplaceSequence(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Create old workspace on a branch.
	if err := env.createWorkspace("old-ws", "feat/shared", []string{"api"}); err != nil {
		t.Fatalf("create old: %v", err)
	}

	// Sanity: creating another workspace on the same branch is normally rejected.
	if err := env.createWorkspace("other", "feat/shared", []string{"api"}); err == nil {
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
	if err := env.createWorkspace("new-ws", "feat/shared", []string{"api"}); err != nil {
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
