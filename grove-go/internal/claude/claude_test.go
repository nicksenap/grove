package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncodePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// The encoding replaces / and . with -
		{"/Users/nick/repos/svc-api", "Users-nick-repos-svc-api"},
		{"/home/user/.grove/workspaces/test/svc-api", "home-user--grove-workspaces-test-svc-api"},
	}
	for _, tt := range tests {
		got := EncodePath(tt.input)
		if got != tt.want {
			t.Errorf("EncodePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMemoryDirFor(t *testing.T) {
	dir := MemoryDirFor("/Users/nick/repos/svc-api")
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %q", dir)
	}
	if filepath.Base(dir) != "memory" {
		t.Errorf("expected 'memory' as last component, got %q", filepath.Base(dir))
	}
}

func TestRehydrate(t *testing.T) {
	// Set up source and destination memory dirs
	srcRepo := filepath.Join(t.TempDir(), "source-repo")
	wtPath := filepath.Join(t.TempDir(), "worktree")

	srcMemDir := MemoryDirFor(srcRepo)
	os.MkdirAll(srcMemDir, 0o755)

	// Create some memory files in source
	os.WriteFile(filepath.Join(srcMemDir, "user.md"), []byte("# User\ntest"), 0o644)
	os.WriteFile(filepath.Join(srcMemDir, "project.md"), []byte("# Project\ntest"), 0o644)

	count, err := Rehydrate(srcRepo, wtPath)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("rehydrated %d files, want 2", count)
	}

	// Verify files exist in destination
	dstMemDir := MemoryDirFor(wtPath)
	if _, err := os.Stat(filepath.Join(dstMemDir, "user.md")); os.IsNotExist(err) {
		t.Error("user.md not copied")
	}

	// Second rehydrate should copy 0 (files already exist)
	count, err = Rehydrate(srcRepo, wtPath)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("second rehydrate copied %d files, want 0", count)
	}
}

func TestHarvest(t *testing.T) {
	srcRepo := filepath.Join(t.TempDir(), "source-repo")
	wtPath := filepath.Join(t.TempDir(), "worktree")

	wtMemDir := MemoryDirFor(wtPath)
	os.MkdirAll(wtMemDir, 0o755)

	// Create memory file in worktree
	os.WriteFile(filepath.Join(wtMemDir, "new-memory.md"), []byte("# New\nfrom worktree"), 0o644)

	count, err := Harvest(wtPath, srcRepo)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("harvested %d files, want 1", count)
	}

	// Verify file exists in source
	srcMemDir := MemoryDirFor(srcRepo)
	if _, err := os.Stat(filepath.Join(srcMemDir, "new-memory.md")); os.IsNotExist(err) {
		t.Error("new-memory.md not harvested")
	}
}

func TestRehydrateNoSource(t *testing.T) {
	// Rehydrate from non-existent source should return 0, nil
	count, err := Rehydrate("/nonexistent/repo", "/some/worktree")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestMigrateMemoryDir(t *testing.T) {
	oldPath := filepath.Join(t.TempDir(), "old-worktree")
	newPath := filepath.Join(t.TempDir(), "new-worktree")

	// Create old dir with content
	oldDir := filepath.Join(claudeProjectsDir(), EncodePath(oldPath))
	os.MkdirAll(filepath.Join(oldDir, "memory"), 0o755)
	os.WriteFile(filepath.Join(oldDir, "memory", "test.md"), []byte("test"), 0o644)

	ok := MigrateMemoryDir(oldPath, newPath)
	if !ok {
		t.Error("expected successful migration")
	}

	// Old dir should be gone
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Error("old dir should not exist after migration")
	}

	// New dir should exist
	newDir := filepath.Join(claudeProjectsDir(), EncodePath(newPath))
	if _, err := os.Stat(filepath.Join(newDir, "memory", "test.md")); os.IsNotExist(err) {
		t.Error("migrated file should exist")
	}

	// Cleanup
	os.RemoveAll(newDir)
}

func TestCleanupOrphanedMemoryDirs(t *testing.T) {
	dir1 := filepath.Join(t.TempDir(), "orphan1")
	dir2 := filepath.Join(t.TempDir(), "orphan2")
	os.MkdirAll(dir1, 0o755)
	os.MkdirAll(dir2, 0o755)

	count := CleanupOrphanedMemoryDirs([]string{dir1, dir2})
	if count != 2 {
		t.Errorf("cleaned up %d, want 2", count)
	}
}
