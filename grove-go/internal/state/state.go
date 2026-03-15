// Package state manages the workspace state file (~/.grove/state.json).
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/models"
)

// StatePath returns the path to ~/.grove/state.json.
func StatePath() string {
	return filepath.Join(config.GroveDir(), "state.json")
}

// LoadWorkspaces reads all workspaces from state.json.
func LoadWorkspaces() ([]models.Workspace, error) {
	data, err := os.ReadFile(StatePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var workspaces []models.Workspace
	if err := json.Unmarshal(data, &workspaces); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}
	return workspaces, nil
}

// SaveWorkspaces atomically persists the workspace list.
func SaveWorkspaces(workspaces []models.Workspace) error {
	data, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(StatePath(), string(data)+"\n")
}

// GetWorkspace returns a workspace by name, or nil if not found.
func GetWorkspace(name string) (*models.Workspace, error) {
	workspaces, err := LoadWorkspaces()
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(name)
	for i := range workspaces {
		if strings.ToLower(workspaces[i].Name) == lower {
			return &workspaces[i], nil
		}
	}
	return nil, nil
}

// AddWorkspace appends a workspace to state.
func AddWorkspace(ws models.Workspace) error {
	workspaces, err := LoadWorkspaces()
	if err != nil {
		return err
	}
	workspaces = append(workspaces, ws)
	return SaveWorkspaces(workspaces)
}

// RemoveWorkspace removes a workspace by name from state.
func RemoveWorkspace(name string) error {
	workspaces, err := LoadWorkspaces()
	if err != nil {
		return err
	}
	lower := strings.ToLower(name)
	filtered := workspaces[:0]
	for _, ws := range workspaces {
		if strings.ToLower(ws.Name) != lower {
			filtered = append(filtered, ws)
		}
	}
	return SaveWorkspaces(filtered)
}

// UpdateWorkspace replaces a workspace in state. If matchName is non-empty,
// it matches by that name instead (for renames).
func UpdateWorkspace(ws models.Workspace, matchName string) error {
	workspaces, err := LoadWorkspaces()
	if err != nil {
		return err
	}

	target := ws.Name
	if matchName != "" {
		target = matchName
	}
	lower := strings.ToLower(target)

	found := false
	for i := range workspaces {
		if strings.ToLower(workspaces[i].Name) == lower {
			workspaces[i] = ws
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("workspace %q not found", target)
	}
	return SaveWorkspaces(workspaces)
}

// FindWorkspaceByPath returns the workspace whose path contains the given directory.
func FindWorkspaceByPath(dir string) (*models.Workspace, error) {
	workspaces, err := LoadWorkspaces()
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	for i := range workspaces {
		wsAbs, _ := filepath.Abs(workspaces[i].Path)
		if strings.HasPrefix(abs, wsAbs+string(filepath.Separator)) || abs == wsAbs {
			return &workspaces[i], nil
		}
	}
	return nil, nil
}

// atomicWrite writes content to a temp file then renames to target.
func atomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".grove-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
