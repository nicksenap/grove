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

// Store manages workspace state persistence.
// Use NewStore for production; create directly in tests.
type Store struct {
	Path string // path to state.json
}

// NewStore creates a Store using the given grove dir.
func NewStore(groveDir string) *Store {
	return &Store{Path: filepath.Join(groveDir, "state.json")}
}

// Load reads all workspaces from state.json.
func (s *Store) Load() ([]models.Workspace, error) {
	data, err := os.ReadFile(s.Path)
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
		return nil, fmt.Errorf("corrupt state file (%s). Run: gw doctor --fix", s.Path)
	}
	return workspaces, nil
}

// Save writes all workspaces to state.json atomically.
func (s *Store) Save(workspaces []models.Workspace) error {
	data, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// GetWorkspace finds a workspace by name.
func (s *Store) GetWorkspace(name string) (*models.Workspace, error) {
	workspaces, err := s.Load()
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
func (s *Store) AddWorkspace(ws models.Workspace) error {
	workspaces, err := s.Load()
	if err != nil {
		return err
	}
	workspaces = append(workspaces, ws)
	return s.Save(workspaces)
}

// UpdateWorkspace replaces a workspace by name.
func (s *Store) UpdateWorkspace(ws models.Workspace) error {
	workspaces, err := s.Load()
	if err != nil {
		return err
	}
	for i := range workspaces {
		if workspaces[i].Name == ws.Name {
			workspaces[i] = ws
			return s.Save(workspaces)
		}
	}
	return fmt.Errorf("workspace %s not found", ws.Name)
}

// RemoveWorkspace removes a workspace by name.
func (s *Store) RemoveWorkspace(name string) error {
	workspaces, err := s.Load()
	if err != nil {
		return err
	}
	filtered := make([]models.Workspace, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws.Name != name {
			filtered = append(filtered, ws)
		}
	}
	return s.Save(filtered)
}

// UpdateWorkspaceByName replaces a workspace matched by matchName.
// This enables atomic renames: ws.Name is already the new name, matchName is the old.
func (s *Store) UpdateWorkspaceByName(ws models.Workspace, matchName string) error {
	workspaces, err := s.Load()
	if err != nil {
		return err
	}
	for i := range workspaces {
		if workspaces[i].Name == matchName {
			workspaces[i] = ws
			return s.Save(workspaces)
		}
	}
	return fmt.Errorf("workspace %s not found", matchName)
}

// RenameWorkspace renames a workspace in state and updates paths.
func (s *Store) RenameWorkspace(oldName, newName, newPath string) error {
	workspaces, err := s.Load()
	if err != nil {
		return err
	}
	for i := range workspaces {
		if workspaces[i].Name == oldName {
			oldPath := workspaces[i].Path
			workspaces[i].Name = newName
			workspaces[i].Path = newPath
			for j := range workspaces[i].Repos {
				workspaces[i].Repos[j].WorktreePath = strings.Replace(
					workspaces[i].Repos[j].WorktreePath, oldPath, newPath, 1,
				)
			}
			return s.Save(workspaces)
		}
	}
	return fmt.Errorf("workspace %s not found", oldName)
}

// FindWorkspaceByPath finds a workspace containing the given path.
func (s *Store) FindWorkspaceByPath(path string) (*models.Workspace, error) {
	workspaces, err := s.Load()
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

// --- Package-level convenience functions using config.GroveDir ---
// These delegate to a Store created from the global config.
// CLI code can use these; test code should create Store directly.

var cachedStore *Store
var cachedStoreDir string

func defaultStore() *Store {
	// Re-create if GroveDir changed (tests patch config.GroveDir)
	if cachedStore == nil || cachedStoreDir != config.GroveDir {
		cachedStore = NewStore(config.GroveDir)
		cachedStoreDir = config.GroveDir
	}
	return cachedStore
}

// StatePath returns the path to state.json.
func StatePath() string           { return defaultStore().Path }
func Load() ([]models.Workspace, error) { return defaultStore().Load() }
func Save(workspaces []models.Workspace) error { return defaultStore().Save(workspaces) }
func GetWorkspace(name string) (*models.Workspace, error) { return defaultStore().GetWorkspace(name) }
func AddWorkspace(ws models.Workspace) error { return defaultStore().AddWorkspace(ws) }
func UpdateWorkspace(ws models.Workspace) error { return defaultStore().UpdateWorkspace(ws) }
func RemoveWorkspace(name string) error { return defaultStore().RemoveWorkspace(name) }
func UpdateWorkspaceByName(ws models.Workspace, matchName string) error {
	return defaultStore().UpdateWorkspaceByName(ws, matchName)
}
func RenameWorkspace(oldName, newName, newPath string) error {
	return defaultStore().RenameWorkspace(oldName, newName, newPath)
}
func FindWorkspaceByPath(path string) (*models.Workspace, error) {
	return defaultStore().FindWorkspaceByPath(path)
}
