package workspace

import (
	"path/filepath"

	"github.com/nicksenap/grove/internal/gitops"
)

func (s *Service) removeWorktree(repo, path string, force bool) error {
	if s.RemoveWorktree != nil {
		return s.RemoveWorktree(repo, path, force)
	}
	return gitops.WorktreeRemove(repo, path, force)
}

func canonicalPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(absolute)
}
