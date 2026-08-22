package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
)

// Rename renames a workspace using a state-first pattern with rollback.
func (s *Service) Rename(oldName, newName string) error {
	return s.State.WithLock(func() error {
		return s.renameLocked(oldName, newName)
	})
}

func (s *Service) renameLocked(oldName, newName string) error {
	ws, err := s.State.GetWorkspace(oldName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %s not found", oldName)
	}

	existing, err := s.State.GetWorkspace(newName)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("workspace %s already exists", newName)
	}

	oldPath := ws.Path
	newPath := filepath.Join(filepath.Dir(oldPath), newName)

	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("directory %s already exists", newPath)
	}

	origName := ws.Name
	origPath := ws.Path
	origWorktreePaths := make([]string, len(ws.Repos))
	for i := range ws.Repos {
		origWorktreePaths[i] = ws.Repos[i].WorktreePath
	}

	ws.Name = newName
	ws.Path = newPath
	for i := range ws.Repos {
		ws.Repos[i].WorktreePath = strings.Replace(ws.Repos[i].WorktreePath, oldPath, newPath, 1)
	}

	if err := s.State.UpdateWorkspaceByName(*ws, oldName); err != nil {
		return err
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		ws.Name = origName
		ws.Path = origPath
		for i := range ws.Repos {
			ws.Repos[i].WorktreePath = origWorktreePaths[i]
		}
		s.State.UpdateWorkspaceByName(*ws, newName)
		return fmt.Errorf("renaming directory: %w", err)
	}

	for _, r := range ws.Repos {
		gitops.WorktreeRepair(r.SourceRepo, r.WorktreePath)
	}

	logging.Info("workspace %q renamed to %q", oldName, newName)
	console.Successf("Workspace %s renamed to %s", oldName, newName)
	return nil
}
