package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/models"
)

// StatePath returns the path to state.json.
func StatePath() string {
	return filepath.Join(config.GroveDir, "state.json")
}

// Load reads all workspaces from state.json.
func Load() ([]models.Workspace, error) {
	data, err := os.ReadFile(StatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Workspace{}, nil
		}
		return nil, fmt.Errorf("reading state: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return []models.Workspace{}, nil
	}

	var workspaces []models.Workspace
	if err := json.Unmarshal(data, &workspaces); err != nil {
		return nil, fmt.Errorf("corrupt state file (%s). Run: gw doctor --fix", StatePath())
	}
	return workspaces, nil
}

// Save writes all workspaces to state.json atomically.
func Save(workspaces []models.Workspace) error {
	data, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		return err
	}

	path := StatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// GetWorkspace finds a workspace by name.
func GetWorkspace(name string) (*models.Workspace, error) {
	workspaces, err := Load()
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		if workspaces[i].Name == name {
			return &workspaces[i], nil
		}
	}
	return nil, nil
}

// AddWorkspace adds a workspace to state.
func AddWorkspace(ws models.Workspace) error {
	workspaces, err := Load()
	if err != nil {
		return err
	}
	workspaces = append(workspaces, ws)
	return Save(workspaces)
}

// UpdateWorkspace replaces a workspace by name.
func UpdateWorkspace(ws models.Workspace) error {
	workspaces, err := Load()
	if err != nil {
		return err
	}
	for i := range workspaces {
		if workspaces[i].Name == ws.Name {
			workspaces[i] = ws
			return Save(workspaces)
		}
	}
	return fmt.Errorf("workspace %s not found", ws.Name)
}

// RemoveWorkspace removes a workspace by name.
func RemoveWorkspace(name string) error {
	workspaces, err := Load()
	if err != nil {
		return err
	}
	filtered := make([]models.Workspace, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws.Name != name {
			filtered = append(filtered, ws)
		}
	}
	return Save(filtered)
}

// UpdateWorkspaceByName replaces a workspace matched by matchName.
// This enables atomic renames: ws.Name is already the new name, matchName is the old.
func UpdateWorkspaceByName(ws models.Workspace, matchName string) error {
	workspaces, err := Load()
	if err != nil {
		return err
	}
	for i := range workspaces {
		if workspaces[i].Name == matchName {
			workspaces[i] = ws
			return Save(workspaces)
		}
	}
	return fmt.Errorf("workspace %s not found", matchName)
}

// RenameWorkspace renames a workspace in state and updates paths.
func RenameWorkspace(oldName, newName, newPath string) error {
	workspaces, err := Load()
	if err != nil {
		return err
	}
	for i := range workspaces {
		if workspaces[i].Name == oldName {
			oldPath := workspaces[i].Path
			workspaces[i].Name = newName
			workspaces[i].Path = newPath
			// Update worktree paths
			for j := range workspaces[i].Repos {
				workspaces[i].Repos[j].WorktreePath = strings.Replace(
					workspaces[i].Repos[j].WorktreePath, oldPath, newPath, 1,
				)
			}
			return Save(workspaces)
		}
	}
	return fmt.Errorf("workspace %s not found", oldName)
}

// FindWorkspaceByPath finds a workspace containing the given path.
func FindWorkspaceByPath(path string) (*models.Workspace, error) {
	workspaces, err := Load()
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	for i := range workspaces {
		wsResolved, err := filepath.EvalSymlinks(workspaces[i].Path)
		if err != nil {
			wsResolved = workspaces[i].Path
		}
		if resolved == wsResolved || strings.HasPrefix(resolved, wsResolved+string(filepath.Separator)) {
			return &workspaces[i], nil
		}
	}
	return nil, nil
}
