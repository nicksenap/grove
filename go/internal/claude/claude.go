// Package claude handles Claude Code memory synchronization between
// source repos and worktrees.
//
// Claude Code stores per-project memory at ~/.claude/projects/<encoded-path>/memory/.
// Since worktrees have different filesystem paths, they get separate memory dirs.
// This package bridges them by copying memory files between source repos and worktrees.
package claude

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// EncodePath encodes a filesystem path the same way Claude Code does:
// replace "/" and "." with "-".
func EncodePath(path string) string {
	s := strings.ReplaceAll(path, "/", "-")
	s = strings.ReplaceAll(s, ".", "-")
	return s
}

// MemoryDirFor returns the memory directory for a given path.
// claudeDir is typically ~/.claude.
func MemoryDirFor(claudeDir, path string) string {
	encoded := EncodePath(path)
	return filepath.Join(claudeDir, "projects", encoded, "memory")
}

// RehydrateMemory copies memory files from source repo to worktree.
// Only copies files that don't already exist in the worktree.
// Returns the number of files copied.
func RehydrateMemory(claudeDir, sourceRepo, worktreePath string) int {
	srcDir := MemoryDirFor(claudeDir, sourceRepo)
	dstDir := MemoryDirFor(claudeDir, worktreePath)

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		dstPath := filepath.Join(dstDir, entry.Name())
		if _, err := os.Stat(dstPath); err == nil {
			// Already exists — skip
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		if copyFile(srcPath, dstPath) == nil {
			count++
		}
	}
	return count
}

// HarvestMemory copies new/newer memory files from worktree back to source.
// Uses mtime comparison: only copies if worktree file is newer than source copy.
// Returns the number of files copied.
func HarvestMemory(claudeDir, worktreePath, sourceRepo string) int {
	wtDir := MemoryDirFor(claudeDir, worktreePath)
	srcDir := MemoryDirFor(claudeDir, sourceRepo)

	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		wtPath := filepath.Join(wtDir, entry.Name())
		srcPath := filepath.Join(srcDir, entry.Name())

		wtInfo, err := os.Stat(wtPath)
		if err != nil {
			continue
		}

		// Check if source exists and is newer
		srcInfo, err := os.Stat(srcPath)
		if err == nil && srcInfo.ModTime().After(wtInfo.ModTime()) {
			// Source is newer — don't overwrite
			continue
		}
		if err == nil && srcInfo.ModTime().Equal(wtInfo.ModTime()) {
			continue
		}

		if copyFile(wtPath, srcPath) == nil {
			count++
		}
	}
	return count
}

// MigrateMemoryDir moves a memory directory from oldPath to newPath.
// If the target already exists, merges files from old into new.
// Returns true if migration succeeded or source didn't exist.
func MigrateMemoryDir(claudeDir, oldPath, newPath string) bool {
	oldProjectDir := filepath.Join(claudeDir, "projects", EncodePath(oldPath))
	newProjectDir := filepath.Join(claudeDir, "projects", EncodePath(newPath))

	if _, err := os.Stat(oldProjectDir); os.IsNotExist(err) {
		return false
	}

	// Check if target exists
	if _, err := os.Stat(newProjectDir); err == nil {
		// Merge: copy files from old memory/ to new memory/
		oldMemDir := filepath.Join(oldProjectDir, "memory")
		newMemDir := filepath.Join(newProjectDir, "memory")
		os.MkdirAll(newMemDir, 0o755)

		entries, _ := os.ReadDir(oldMemDir)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			dst := filepath.Join(newMemDir, entry.Name())
			if _, err := os.Stat(dst); err == nil {
				continue // don't overwrite existing
			}
			copyFile(filepath.Join(oldMemDir, entry.Name()), dst)
		}
		os.RemoveAll(oldProjectDir)
	} else {
		// Simple rename
		os.MkdirAll(filepath.Dir(newProjectDir), 0o755)
		if err := os.Rename(oldProjectDir, newProjectDir); err != nil {
			return false
		}
	}

	return true
}

// FindOrphanedMemoryDirs finds Claude project dirs for worktree paths that
// no longer exist. Uses the workspace dir prefix to scope the search (avoids
// flagging non-grove Claude projects), and cross-references against active
// worktree paths rather than attempting lossy path reconstruction.
func FindOrphanedMemoryDirs(claudeDir string, workspaceDir string) []string {
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}

	// Encoded workspace dir prefix for scoping
	wsDirEncoded := EncodePath(workspaceDir)

	// Walk the workspace dir to find all paths that actually exist
	activePaths := make(map[string]bool)
	filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			activePaths[EncodePath(path)] = true
		}
		return nil
	})

	var orphaned []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		projDir := filepath.Join(projectsDir, name)

		// Only consider dirs that have a memory/ subdirectory
		memDir := filepath.Join(projDir, "memory")
		if _, err := os.Stat(memDir); os.IsNotExist(err) {
			continue
		}

		// Only consider dirs scoped to the workspace directory
		if !strings.HasPrefix(name, wsDirEncoded) {
			continue
		}

		// If this encoded path matches an active directory, it's not orphaned
		if activePaths[name] {
			continue
		}

		orphaned = append(orphaned, projDir)
	}

	return orphaned
}

// copyFile copies src to dst atomically, creating parent directories as needed.
func copyFile(src, dst string) error {
	os.MkdirAll(filepath.Dir(dst), 0o755)

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()

	return os.Rename(tmp, dst)
}
