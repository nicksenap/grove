// Package claude handles Claude Code memory synchronization between
// source repos and worktrees.
package claude

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// claudeProjectsDir returns ~/.claude/projects.
func claudeProjectsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// EncodePath encodes a filesystem path for use as a Claude project directory name.
// Replaces "/" and "." with "-", matching Claude Code's scheme.
func EncodePath(path string) string {
	// Use absolute path
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	encoded := strings.ReplaceAll(abs, string(filepath.Separator), "-")
	encoded = strings.ReplaceAll(encoded, ".", "-")
	// Remove leading dash (from leading /)
	encoded = strings.TrimLeft(encoded, "-")
	return encoded
}

// MemoryDirFor returns the Claude memory directory for a given path.
func MemoryDirFor(path string) string {
	return filepath.Join(claudeProjectsDir(), EncodePath(path), "memory")
}

// Rehydrate copies memory files from source repo's Claude dir to worktree's Claude dir.
// Only copies files that don't already exist in the destination.
// Returns the count of files copied.
func Rehydrate(sourceRepo, worktreePath string) (int, error) {
	srcDir := MemoryDirFor(sourceRepo)
	dstDir := MemoryDirFor(worktreePath)

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		srcFile := filepath.Join(srcDir, entry.Name())
		dstFile := filepath.Join(dstDir, entry.Name())

		// Skip if destination already exists
		if _, err := os.Stat(dstFile); err == nil {
			continue
		}

		if err := copyFile(srcFile, dstFile); err != nil {
			continue
		}
		count++
	}
	return count, nil
}

// Harvest copies memory files from worktree's Claude dir back to source repo's Claude dir.
// Copies new files and overwrites files that are newer by mtime.
// Returns the count of files copied.
func Harvest(worktreePath, sourceRepo string) (int, error) {
	srcDir := MemoryDirFor(worktreePath)
	dstDir := MemoryDirFor(sourceRepo)

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		srcFile := filepath.Join(srcDir, entry.Name())
		dstFile := filepath.Join(dstDir, entry.Name())

		srcInfo, err := entry.Info()
		if err != nil {
			continue
		}

		// Copy if destination doesn't exist or source is newer
		if dstInfo, err := os.Stat(dstFile); err == nil {
			if !srcInfo.ModTime().After(dstInfo.ModTime()) {
				continue
			}
		}

		if err := copyFile(srcFile, dstFile); err != nil {
			continue
		}
		count++
	}
	return count, nil
}

// MigrateMemoryDir renames a Claude project directory (for workspace renames).
func MigrateMemoryDir(oldPath, newPath string) bool {
	oldDir := filepath.Join(claudeProjectsDir(), EncodePath(oldPath))
	newDir := filepath.Join(claudeProjectsDir(), EncodePath(newPath))

	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return false
	}

	if err := os.Rename(oldDir, newDir); err != nil {
		return false
	}
	return true
}

// FindOrphanedMemoryDirs finds Claude memory directories for worktree paths
// that no longer exist on disk.
func FindOrphanedMemoryDirs(activeWorktreePaths []string) []string {
	projDir := claudeProjectsDir()
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return nil
	}

	activeSet := make(map[string]bool)
	for _, p := range activeWorktreePaths {
		activeSet[EncodePath(p)] = true
	}

	var orphaned []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		encoded := entry.Name()
		memDir := filepath.Join(projDir, encoded, "memory")
		if _, err := os.Stat(memDir); os.IsNotExist(err) {
			continue
		}

		// Check if this is a worktree path (skip source repos)
		if activeSet[encoded] {
			continue
		}

		// Decode path and check if it exists on disk
		// If the directory doesn't exist, it's orphaned
		fullPath := filepath.Join(projDir, encoded)
		if isOrphaned(fullPath, encoded) {
			orphaned = append(orphaned, fullPath)
		}
	}
	return orphaned
}

func isOrphaned(projDir, encoded string) bool {
	// Reconstruct the original path from the encoded name
	// This is best-effort since encoding is lossy
	memDir := filepath.Join(projDir, "memory")
	if _, err := os.Stat(memDir); os.IsNotExist(err) {
		return false
	}
	return true
}

// CleanupOrphanedMemoryDirs removes orphaned Claude memory directories.
func CleanupOrphanedMemoryDirs(dirs []string) int {
	count := 0
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err == nil {
			count++
		}
	}
	return count
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
