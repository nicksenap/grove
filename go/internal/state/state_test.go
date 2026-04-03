package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/models"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	groveDir := filepath.Join(dir, ".grove")
	os.MkdirAll(groveDir, 0o755)
	s := NewStore(groveDir)
	os.WriteFile(s.Path, []byte("[]"), 0o644)
	return s
}

func TestLoadEmpty(t *testing.T) {
	s := testStore(t)

	workspaces, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(workspaces) != 0 {
		t.Errorf("expected empty, got %d workspaces", len(workspaces))
	}
}

func TestLoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, ".grove"))

	workspaces, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(workspaces) != 0 {
		t.Errorf("expected empty, got %d workspaces", len(workspaces))
	}
}

func TestAddAndGet(t *testing.T) {
	s := testStore(t)

	ws := models.NewWorkspace("test", "/tmp/test", "feat/test")
	ws.Repos = []models.RepoWorktree{
		{RepoName: "api", SourceRepo: "/src/api", WorktreePath: "/tmp/test/api", Branch: "feat/test"},
	}

	if err := s.AddWorkspace(ws); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := s.GetWorkspace("test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected workspace, got nil")
	}
	if got.Name != "test" {
		t.Errorf("name: got %q, want 'test'", got.Name)
	}
	if len(got.Repos) != 1 {
		t.Errorf("repos: got %d, want 1", len(got.Repos))
	}
}

func TestGetNonexistent(t *testing.T) {
	s := testStore(t)

	got, err := s.GetWorkspace("nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestRemove(t *testing.T) {
	s := testStore(t)

	ws := models.NewWorkspace("test", "/tmp/test", "main")
	s.AddWorkspace(ws)

	if err := s.RemoveWorkspace("test"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got, _ := s.GetWorkspace("test")
	if got != nil {
		t.Error("expected nil after removal")
	}
}

func TestRemoveNonexistentIsNoop(t *testing.T) {
	s := testStore(t)

	if err := s.RemoveWorkspace("nonexistent"); err != nil {
		t.Fatalf("remove nonexistent: %v", err)
	}
}

func TestMultipleWorkspaces(t *testing.T) {
	s := testStore(t)

	ws1 := models.NewWorkspace("ws1", "/tmp/ws1", "feat/a")
	ws2 := models.NewWorkspace("ws2", "/tmp/ws2", "feat/b")
	s.AddWorkspace(ws1)
	s.AddWorkspace(ws2)

	all, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(all))
	}

	got1, _ := s.GetWorkspace("ws1")
	got2, _ := s.GetWorkspace("ws2")
	if got1 == nil || got2 == nil {
		t.Fatal("expected both workspaces to exist")
	}
	if got1.Branch != "feat/a" || got2.Branch != "feat/b" {
		t.Error("workspace branches don't match")
	}
}

func TestUpdateWorkspace(t *testing.T) {
	s := testStore(t)

	ws := models.NewWorkspace("test", "/tmp/test", "main")
	s.AddWorkspace(ws)

	ws.Branch = "updated"
	if err := s.UpdateWorkspace(ws); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := s.GetWorkspace("test")
	if got.Branch != "updated" {
		t.Errorf("branch: got %q, want 'updated'", got.Branch)
	}
}

func TestUpdateNonexistent(t *testing.T) {
	s := testStore(t)

	ws := models.NewWorkspace("nonexistent", "/tmp/test", "main")
	err := s.UpdateWorkspace(ws)
	if err == nil {
		t.Error("expected error for nonexistent workspace")
	}
}

func TestRenameWorkspace(t *testing.T) {
	s := testStore(t)

	ws := models.NewWorkspace("old", "/tmp/old", "main")
	ws.Repos = []models.RepoWorktree{
		{RepoName: "api", SourceRepo: "/src/api", WorktreePath: "/tmp/old/api", Branch: "main"},
	}
	s.AddWorkspace(ws)

	if err := s.RenameWorkspace("old", "new", "/tmp/new"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	got, _ := s.GetWorkspace("old")
	if got != nil {
		t.Error("old name should not exist")
	}

	got, _ = s.GetWorkspace("new")
	if got == nil {
		t.Fatal("new workspace not found")
	}
	if got.Path != "/tmp/new" {
		t.Errorf("path: got %q, want '/tmp/new'", got.Path)
	}
	if got.Repos[0].WorktreePath != "/tmp/new/api" {
		t.Errorf("worktree path: got %q, want '/tmp/new/api'", got.Repos[0].WorktreePath)
	}
}

func TestRenameNonexistent(t *testing.T) {
	s := testStore(t)

	err := s.RenameWorkspace("nonexistent", "new", "/tmp/new")
	if err == nil {
		t.Error("expected error for nonexistent workspace")
	}
}

func TestFindWorkspaceByExactPath(t *testing.T) {
	s := testStore(t)

	wsPath := filepath.Join(t.TempDir(), "ws-find")
	os.MkdirAll(wsPath, 0o755)

	ws := models.NewWorkspace("findme", wsPath, "main")
	s.AddWorkspace(ws)

	got, err := s.FindWorkspaceByPath(wsPath)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil {
		t.Fatal("expected to find workspace")
	}
	if got.Name != "findme" {
		t.Errorf("name: got %q, want 'findme'", got.Name)
	}
}

func TestFindWorkspaceBySubdir(t *testing.T) {
	s := testStore(t)

	wsPath := filepath.Join(t.TempDir(), "ws-sub")
	subDir := filepath.Join(wsPath, "api", "src")
	os.MkdirAll(subDir, 0o755)

	ws := models.NewWorkspace("subtest", wsPath, "main")
	s.AddWorkspace(ws)

	got, err := s.FindWorkspaceByPath(subDir)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil {
		t.Fatal("expected to find workspace from subdir")
	}
	if got.Name != "subtest" {
		t.Errorf("name: got %q, want 'subtest'", got.Name)
	}
}

func TestFindWorkspaceByPathNotFound(t *testing.T) {
	s := testStore(t)

	got, err := s.FindWorkspaceByPath("/completely/unrelated/path")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestStatePersistsAsJSON(t *testing.T) {
	s := testStore(t)

	ws := models.NewWorkspace("json-test", "/tmp/json", "main")
	s.AddWorkspace(ws)

	data, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	content := string(data)
	if content[0] != '[' {
		t.Errorf("expected JSON array, got: %s", content[:20])
	}
}

func TestAtomicWrite(t *testing.T) {
	s := testStore(t)

	ws := models.NewWorkspace("atomic", "/tmp/atomic", "main")
	s.AddWorkspace(ws)

	tmpPath := s.Path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up")
	}
}
