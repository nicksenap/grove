package workspace

import (
	"context"
	"os"
	"os/exec"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
)

// stateStore is the authoritative workspace-state boundary used by the service.
// It is an interface so tests can wrap the real *state.Store and inject commit
// failures (including "commit applied, then error") without a global failpoint.
type stateStore interface {
	Load() ([]models.Workspace, error)
	GetWorkspace(name string) (*models.Workspace, error)
	FindWorkspaceByPath(path string) (*models.Workspace, error)
	AddWorkspace(ws models.Workspace) error
	UpdateWorkspace(ws models.Workspace) error
	UpdateWorkspaceByName(ws models.Workspace, matchName string) error
	RemoveWorkspace(name string) error
	RenameWorkspace(oldName, newName, newPath string) error
	WithMutation(ctx context.Context, fn func(*state.Mutation) error) error
	AcquireWorkspaceLock(ctx context.Context, name string) (func(), error)
	AcquireResourceLock(ctx context.Context, key string) (func(), error)
}

// journalStore is the recovery-journal boundary. An interface so tests can
// inject record write/delete failures per Service instance.
type journalStore interface {
	Write(rec *state.OperationRecord) error
	Read(id string) (*state.OperationRecord, error)
	List() ([]state.OperationRecord, error)
	Delete(id string) error
}

// Service orchestrates workspace operations with injectable dependencies.
type Service struct {
	State        stateStore
	Stats        *stats.Tracker
	Ops          journalStore
	RunCmd       func(dir, cmd string) error
	RunCmdSilent func(dir, cmd string) error

	// backend performs low-level Git/filesystem mutations. It is unexported so
	// only same-package test code can install a fault-injecting wrapper; release
	// binaries always use the production backend.
	backend mutationBackend

	// commitFault is a test-only seam intercepting the state commit boundary
	// (see withMutation). nil in production.
	commitFault func(*state.Mutation) error
}

// NewService creates a Service with production dependencies.
func NewService() *Service {
	return &Service{
		State:        state.NewStore(config.GroveDir),
		Stats:        stats.NewTracker(config.GroveDir),
		Ops:          state.NewOperationStore(config.GroveDir),
		RunCmd:       prodRunCmd,
		RunCmdSilent: prodRunCmdSilent,
		backend:      prodBackend{},
	}
}

// ops returns the recovery journal. It is always set by NewService and by test
// constructors; a nil journal is a programming error surfaced eagerly.
func (s *Service) ops() journalStore {
	if s.Ops == nil {
		panic("workspace.Service: Ops journal not configured")
	}
	return s.Ops
}

func prodRunCmd(dir, cmdStr string) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func prodRunCmdSilent(dir, cmdStr string) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	return cmd.Run()
}
