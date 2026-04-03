package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/Users/nick/.grove/workspaces/feat-foo", "-Users-nick--grove-workspaces-feat-foo"},
		{"/home/user/dev/api", "-home-user-dev-api"},
		{"/tmp/test", "-tmp-test"},
	}
	for _, tt := range tests {
		got := EncodePath(tt.input)
		if got != tt.want {
			t.Errorf("EncodePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMemoryDirFor(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	dir := MemoryDirFor(claudeDir, "/Users/nick/dev/api")
	if dir == "" {
		t.Error("expected non-empty dir")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %q", dir)
	}
}

func TestRehydrateMemory(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	sourceRepo := "/fake/source/repo"
	worktreePath := "/fake/worktree/path"

	// Create source memory with a file
	sourceMemDir := MemoryDirFor(claudeDir, sourceRepo)
	os.MkdirAll(sourceMemDir, 0o755)
	os.WriteFile(filepath.Join(sourceMemDir, "context.md"), []byte("source data"), 0o644)

	// Rehydrate should copy to worktree memory
	count := RehydrateMemory(claudeDir, sourceRepo, worktreePath)
	if count != 1 {
		t.Errorf("expected 1 file copied, got %d", count)
	}

	// File should exist in worktree memory dir
	wtMemDir := MemoryDirFor(claudeDir, worktreePath)
	data, err := os.ReadFile(filepath.Join(wtMemDir, "context.md"))
	if err != nil {
		t.Fatalf("file not copied: %v", err)
	}
	if string(data) != "source data" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestRehydrateSkipsExisting(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	sourceRepo := "/fake/source"
	worktreePath := "/fake/worktree"

	// Create source memory
	sourceMemDir := MemoryDirFor(claudeDir, sourceRepo)
	os.MkdirAll(sourceMemDir, 0o755)
	os.WriteFile(filepath.Join(sourceMemDir, "existing.md"), []byte("source"), 0o644)

	// Create worktree memory with the same file (different content)
	wtMemDir := MemoryDirFor(claudeDir, worktreePath)
	os.MkdirAll(wtMemDir, 0o755)
	os.WriteFile(filepath.Join(wtMemDir, "existing.md"), []byte("worktree version"), 0o644)

	// Rehydrate should skip since file exists
	count := RehydrateMemory(claudeDir, sourceRepo, worktreePath)
	if count != 0 {
		t.Errorf("expected 0 files copied (skip existing), got %d", count)
	}

	// Content should be unchanged
	data, _ := os.ReadFile(filepath.Join(wtMemDir, "existing.md"))
	if string(data) != "worktree version" {
		t.Error("existing file should not be overwritten")
	}
}

func TestRehydrateNoSource(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	count := RehydrateMemory(claudeDir, "/no/source", "/no/worktree")
	if count != 0 {
		t.Errorf("expected 0 for missing source, got %d", count)
	}
}

func TestHarvestMemory(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	sourceRepo := "/fake/source"
	worktreePath := "/fake/worktree"

	// Create worktree memory with a new file
	wtMemDir := MemoryDirFor(claudeDir, worktreePath)
	os.MkdirAll(wtMemDir, 0o755)
	os.WriteFile(filepath.Join(wtMemDir, "new-learning.md"), []byte("learned something"), 0o644)

	// Harvest should copy back to source
	count := HarvestMemory(claudeDir, worktreePath, sourceRepo)
	if count != 1 {
		t.Errorf("expected 1 file harvested, got %d", count)
	}

	sourceMemDir := MemoryDirFor(claudeDir, sourceRepo)
	data, err := os.ReadFile(filepath.Join(sourceMemDir, "new-learning.md"))
	if err != nil {
		t.Fatalf("file not harvested: %v", err)
	}
	if string(data) != "learned something" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestHarvestOverwritesOlder(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	sourceRepo := "/fake/source"
	worktreePath := "/fake/worktree"

	// Create source memory with an old file
	sourceMemDir := MemoryDirFor(claudeDir, sourceRepo)
	os.MkdirAll(sourceMemDir, 0o755)
	os.WriteFile(filepath.Join(sourceMemDir, "evolving.md"), []byte("old"), 0o644)
	// Set old mtime
	oldTime := time.Now().Add(-1 * time.Hour)
	os.Chtimes(filepath.Join(sourceMemDir, "evolving.md"), oldTime, oldTime)

	// Create worktree memory with newer version
	wtMemDir := MemoryDirFor(claudeDir, worktreePath)
	os.MkdirAll(wtMemDir, 0o755)
	os.WriteFile(filepath.Join(wtMemDir, "evolving.md"), []byte("new"), 0o644)

	count := HarvestMemory(claudeDir, worktreePath, sourceRepo)
	if count != 1 {
		t.Errorf("expected 1 file harvested, got %d", count)
	}

	data, _ := os.ReadFile(filepath.Join(sourceMemDir, "evolving.md"))
	if string(data) != "new" {
		t.Errorf("should overwrite older: got %q", string(data))
	}
}

func TestHarvestPreservesNewer(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	sourceRepo := "/fake/source"
	worktreePath := "/fake/worktree"

	// Create source memory with a NEWER file
	sourceMemDir := MemoryDirFor(claudeDir, sourceRepo)
	os.MkdirAll(sourceMemDir, 0o755)
	os.WriteFile(filepath.Join(sourceMemDir, "recent.md"), []byte("fresh"), 0o644)

	// Create worktree memory with an OLDER version
	wtMemDir := MemoryDirFor(claudeDir, worktreePath)
	os.MkdirAll(wtMemDir, 0o755)
	os.WriteFile(filepath.Join(wtMemDir, "recent.md"), []byte("stale"), 0o644)
	oldTime := time.Now().Add(-1 * time.Hour)
	os.Chtimes(filepath.Join(wtMemDir, "recent.md"), oldTime, oldTime)

	count := HarvestMemory(claudeDir, worktreePath, sourceRepo)
	if count != 0 {
		t.Errorf("expected 0 (preserve newer), got %d", count)
	}

	data, _ := os.ReadFile(filepath.Join(sourceMemDir, "recent.md"))
	if string(data) != "fresh" {
		t.Error("newer source should not be overwritten")
	}
}

func TestHarvestNoWorktreeMemory(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	count := HarvestMemory(claudeDir, "/no/worktree", "/no/source")
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestMigrateMemoryDir(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	oldPath := "/fake/old/path"
	newPath := "/fake/new/path"

	// Create memory for old path
	oldMemDir := MemoryDirFor(claudeDir, oldPath)
	os.MkdirAll(oldMemDir, 0o755)
	os.WriteFile(filepath.Join(oldMemDir, "data.md"), []byte("content"), 0o644)

	ok := MigrateMemoryDir(claudeDir, oldPath, newPath)
	if !ok {
		t.Error("migration should succeed")
	}

	// Old should be gone
	oldProjectDir := filepath.Dir(oldMemDir) // project dir (parent of memory/)
	if _, err := os.Stat(oldProjectDir); !os.IsNotExist(err) {
		t.Error("old project dir should be removed")
	}

	// New should exist
	newMemDir := MemoryDirFor(claudeDir, newPath)
	data, err := os.ReadFile(filepath.Join(newMemDir, "data.md"))
	if err != nil {
		t.Fatalf("migrated file missing: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestMigrateMemoryDirMerge(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	oldPath := "/fake/old"
	newPath := "/fake/new"

	// Create memory for both
	oldMemDir := MemoryDirFor(claudeDir, oldPath)
	os.MkdirAll(oldMemDir, 0o755)
	os.WriteFile(filepath.Join(oldMemDir, "old-only.md"), []byte("from old"), 0o644)

	newMemDir := MemoryDirFor(claudeDir, newPath)
	os.MkdirAll(newMemDir, 0o755)
	os.WriteFile(filepath.Join(newMemDir, "new-only.md"), []byte("from new"), 0o644)

	ok := MigrateMemoryDir(claudeDir, oldPath, newPath)
	if !ok {
		t.Error("merge migration should succeed")
	}

	// Both files should exist in new
	if _, err := os.Stat(filepath.Join(newMemDir, "old-only.md")); os.IsNotExist(err) {
		t.Error("old file should be merged into new")
	}
	if _, err := os.Stat(filepath.Join(newMemDir, "new-only.md")); os.IsNotExist(err) {
		t.Error("new file should be preserved")
	}
}

func TestMigrateNoSource(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")

	ok := MigrateMemoryDir(claudeDir, "/no/exist", "/fake/new")
	if ok {
		t.Error("expected false for missing source")
	}
}
