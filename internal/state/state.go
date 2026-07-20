package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/models"
)

// lockTimeout bounds how long a mutation waits for the advisory state lock
// before returning a retryable STATE_LOCK_TIMEOUT.
const lockTimeout = 30 * time.Second

// Store manages workspace state persistence.
// Use NewStore for production; create directly in tests.
type Store struct {
	Path string // path to state.json

	// Test-only fault-injection seams. They are unexported, so only same-package
	// test code can set them; release binaries can never enable a failpoint.
	failWrite   func() error // injected before writing temp bytes
	failSync    func() error // injected before fsyncing the temp file
	failRename  func() error // injected before the atomic rename
	failDirSync func() error // injected before fsyncing the parent directory
}

// NewStore creates a Store using the given grove dir.
func NewStore(groveDir string) *Store {
	return &Store{Path: filepath.Join(groveDir, "state.json")}
}

// dir returns the directory containing state.json.
func (s *Store) dir() string { return filepath.Dir(s.Path) }

// lockPath is the advisory lock file guarding all state mutations. It is never
// unlinked so that flock ownership stays stable across processes.
func (s *Store) lockPath() string { return filepath.Join(s.dir(), "state.lock") }

// Load reads all workspaces from state.json. Reads are lock-free: writes use an
// atomic rename so a reader always observes a complete snapshot.
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

// writeSnapshot durably writes workspaces to state.json.
func (s *Store) writeSnapshot(workspaces []models.Workspace) error {
	data, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return err
	}
	return s.writeFileDurable(s.Path, data, 0o644)
}

// WithMutation acquires the advisory lock, loads the authoritative snapshot,
// and passes a caller-owned Mutation handle. The lock is held for the entire
// callback and released afterward. The handle is invalidated before the lock is
// released, so an escaped handle cannot commit outside serialization. Service
// code must never call a public lock-acquiring mutator while holding a handle;
// a nested call from the same goroutine is rejected immediately.
func (s *Store) WithMutation(ctx context.Context, fn func(*Mutation) error) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Reject same-goroutine reentrancy immediately rather than self-deadlocking
	// on the advisory lock for the full timeout.
	lp := s.lockPath()
	if heldByCurrentGoroutine(lp) {
		return &CodedError{
			Code:    CodeStateNested,
			Message: "nested state mutation is not allowed; use the existing Mutation handle",
		}
	}

	// The lock file lives in the grove dir, which may not exist on first use.
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return err
	}

	lock, err := acquireLock(ctx, lp, lockTimeout)
	if err != nil {
		if errors.Is(err, errLockTimeout) {
			return &CodedError{
				Code:      CodeStateLockTimeout,
				Message:   fmt.Sprintf("could not acquire state lock within %s", lockTimeout),
				Retryable: true,
			}
		}
		return err
	}
	markHeld(lp)
	defer func() {
		clearHeld(lp)
		if rerr := lock.release(); rerr != nil && err == nil {
			err = rerr
		}
	}()

	workspaces, lerr := s.Load()
	if lerr != nil {
		return lerr
	}
	m := &Mutation{store: s, workspaces: workspaces, active: true}
	defer func() { m.active = false }() // invalidate before lock release
	return fn(m)
}

// Mutation is a caller-owned handle to a locked state snapshot. All reads and
// edits operate on an in-memory copy until Commit durably persists it. A
// Mutation is only valid for the duration of the WithMutation callback.
type Mutation struct {
	store      *Store
	workspaces []models.Workspace
	active     bool
	committed  bool
}

// errInactive is returned when a Mutation is used outside its callback.
func (m *Mutation) checkActive() error {
	if !m.active {
		return &CodedError{
			Code:    CodeStateInactiveHandle,
			Message: "mutation handle used outside its WithMutation callback",
		}
	}
	return nil
}

// cloneWorkspace returns a deep copy so callers cannot mutate the locked
// snapshot through returned aliases.
func cloneWorkspace(ws models.Workspace) models.Workspace {
	out := ws
	out.Repos = append([]models.RepoWorktree(nil), ws.Repos...)
	if ws.Source != nil {
		src := *ws.Source
		out.Source = &src
	}
	return out
}

// Workspaces returns a deep copy of the current in-memory snapshot (post-edit).
func (m *Mutation) Workspaces() []models.Workspace {
	out := make([]models.Workspace, len(m.workspaces))
	for i := range m.workspaces {
		out[i] = cloneWorkspace(m.workspaces[i])
	}
	return out
}

// Get returns a deep copy of the workspace by name, or nil if absent.
func (m *Mutation) Get(name string) *models.Workspace {
	for i := range m.workspaces {
		if m.workspaces[i].Name == name {
			ws := cloneWorkspace(m.workspaces[i])
			return &ws
		}
	}
	return nil
}

// Exists reports whether a workspace with the given name is present.
func (m *Mutation) Exists(name string) bool {
	for i := range m.workspaces {
		if m.workspaces[i].Name == name {
			return true
		}
	}
	return false
}

// Add appends a workspace to the in-memory snapshot. It returns a conflict
// CodedError if a workspace with the same name already exists.
func (m *Mutation) Add(ws models.Workspace) error {
	if err := m.checkActive(); err != nil {
		return err
	}
	if m.Exists(ws.Name) {
		return &CodedError{
			Code:    CodeStateConflict,
			Message: fmt.Sprintf("workspace %q already exists", ws.Name),
		}
	}
	m.workspaces = append(m.workspaces, ws)
	return nil
}

// Update replaces a workspace matched by name.
func (m *Mutation) Update(ws models.Workspace) error {
	return m.UpdateByName(ws, ws.Name)
}

// UpdateByName replaces a workspace matched by matchName (enables renames where
// ws.Name is already the new name). If the new name differs from matchName and
// already belongs to another workspace, a conflict is returned.
func (m *Mutation) UpdateByName(ws models.Workspace, matchName string) error {
	if err := m.checkActive(); err != nil {
		return err
	}
	if ws.Name != matchName && m.Exists(ws.Name) {
		return &CodedError{
			Code:    CodeStateConflict,
			Message: fmt.Sprintf("workspace %q already exists", ws.Name),
		}
	}
	for i := range m.workspaces {
		if m.workspaces[i].Name == matchName {
			m.workspaces[i] = ws
			return nil
		}
	}
	return fmt.Errorf("workspace %s not found", matchName)
}

// Remove deletes a workspace by name (no error if absent).
func (m *Mutation) Remove(name string) error {
	if err := m.checkActive(); err != nil {
		return err
	}
	filtered := make([]models.Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		if ws.Name != name {
			filtered = append(filtered, ws)
		}
	}
	m.workspaces = filtered
	return nil
}

// Commit durably persists the in-memory snapshot. It is idempotent within a
// single mutation and fails if the handle is no longer active.
func (m *Mutation) Commit() error {
	if err := m.checkActive(); err != nil {
		return err
	}
	if m.committed {
		return nil
	}
	if err := m.store.writeSnapshot(m.workspaces); err != nil {
		return err
	}
	m.committed = true
	return nil
}

// --- Public Store mutators: thin wrappers over WithMutation ---

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
	return s.WithMutation(context.Background(), func(m *Mutation) error {
		if err := m.Add(ws); err != nil {
			return err
		}
		return m.Commit()
	})
}

// UpdateWorkspace replaces a workspace by name.
func (s *Store) UpdateWorkspace(ws models.Workspace) error {
	return s.WithMutation(context.Background(), func(m *Mutation) error {
		if err := m.Update(ws); err != nil {
			return err
		}
		return m.Commit()
	})
}

// RemoveWorkspace removes a workspace by name.
func (s *Store) RemoveWorkspace(name string) error {
	return s.WithMutation(context.Background(), func(m *Mutation) error {
		if err := m.Remove(name); err != nil {
			return err
		}
		return m.Commit()
	})
}

// UpdateWorkspaceByName replaces a workspace matched by matchName.
// This enables atomic renames: ws.Name is already the new name, matchName is the old.
func (s *Store) UpdateWorkspaceByName(ws models.Workspace, matchName string) error {
	return s.WithMutation(context.Background(), func(m *Mutation) error {
		if err := m.UpdateByName(ws, matchName); err != nil {
			return err
		}
		return m.Commit()
	})
}

// RenameWorkspace renames a workspace in state and updates paths.
func (s *Store) RenameWorkspace(oldName, newName, newPath string) error {
	return s.WithMutation(context.Background(), func(m *Mutation) error {
		if oldName != newName && m.Exists(newName) {
			return &CodedError{
				Code:    CodeStateConflict,
				Message: fmt.Sprintf("workspace %q already exists", newName),
			}
		}
		for i := range m.workspaces {
			if m.workspaces[i].Name == oldName {
				oldPath := m.workspaces[i].Path
				m.workspaces[i].Name = newName
				m.workspaces[i].Path = newPath
				for j := range m.workspaces[i].Repos {
					m.workspaces[i].Repos[j].WorktreePath = strings.Replace(
						m.workspaces[i].Repos[j].WorktreePath, oldPath, newPath, 1,
					)
				}
				return m.Commit()
			}
		}
		return fmt.Errorf("workspace %s not found", oldName)
	})
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
func StatePath() string                                   { return defaultStore().Path }
func Load() ([]models.Workspace, error)                   { return defaultStore().Load() }
func GetWorkspace(name string) (*models.Workspace, error) { return defaultStore().GetWorkspace(name) }
func AddWorkspace(ws models.Workspace) error              { return defaultStore().AddWorkspace(ws) }
func UpdateWorkspace(ws models.Workspace) error           { return defaultStore().UpdateWorkspace(ws) }
func RemoveWorkspace(name string) error                   { return defaultStore().RemoveWorkspace(name) }
func UpdateWorkspaceByName(ws models.Workspace, matchName string) error {
	return defaultStore().UpdateWorkspaceByName(ws, matchName)
}
func RenameWorkspace(oldName, newName, newPath string) error {
	return defaultStore().RenameWorkspace(oldName, newName, newPath)
}
func FindWorkspaceByPath(path string) (*models.Workspace, error) {
	return defaultStore().FindWorkspaceByPath(path)
}
