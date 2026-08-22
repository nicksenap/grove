// Package operations owns user-facing workspace operation ordering, including
// global lifecycle hook policy. The workspace package remains lifecycle-free.
package operations

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/lifecycle"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
)

// WorkspaceService contains the lifecycle-free workspace primitives used by
// operation orchestration.
type WorkspaceService interface {
	CreateWithOpts(name string, opts workspace.CreateOpts) error
	CreateWithPreparation(name string, opts workspace.PreparationOpts, prepare func(models.Workspace) error) error
	DeleteWithOptions(name string, opts workspace.RemoveOptions) error
}

// WorkspaceStore reads the completed workspace used to build lifecycle context.
type WorkspaceStore interface {
	GetWorkspace(name string) (*models.Workspace, error)
}

// Service coordinates workspace primitives and global lifecycle hooks.
type Service struct {
	Workspace WorkspaceService
	Store     WorkspaceStore
	RunHook   func(string, lifecycle.Vars) error
	Warn      func(string)
}

// NewService creates an operation service with production dependencies.
func NewService() *Service {
	workspaceService := workspace.NewService()
	return &Service{
		Workspace: workspaceService,
		Store:     workspaceService.State,
		RunHook:   lifecycle.Run,
		Warn:      console.Warning,
	}
}

// Preparation configures the Recipe preparation path for Create.
type Preparation struct {
	Options workspace.PreparationOpts
	Run     func(models.Workspace) error
}

// CreateRequest is the typed input for a workspace creation operation.
type CreateRequest struct {
	Name        string
	Options     workspace.CreateOpts
	Preparation *Preparation
}

// CreateResult reports whether the workspace was fully created. Created remains
// true when an aborting post_create hook fails because creation is not rolled back.
type CreateResult struct {
	Created   bool
	Workspace models.Workspace
}

// HookError identifies the lifecycle phase that aborted an operation.
type HookError struct {
	Hook string
	Err  error
}

func (e *HookError) Error() string { return e.Err.Error() }
func (e *HookError) Unwrap() error { return e.Err }

// Create provisions a workspace, then applies the shared post_create policy.
func (s *Service) Create(req CreateRequest) (CreateResult, error) {
	var err error
	if req.Preparation == nil {
		err = s.Workspace.CreateWithOpts(req.Name, req.Options)
	} else {
		err = s.Workspace.CreateWithPreparation(req.Name, req.Preparation.Options, req.Preparation.Run)
	}
	if err != nil {
		return CreateResult{}, err
	}

	result := CreateResult{Created: true, Workspace: expectedWorkspace(req)}
	ws, err := s.Store.GetWorkspace(req.Name)
	if err != nil {
		return result, fmt.Errorf("loading created workspace: %w", err)
	}
	if ws == nil {
		return result, fmt.Errorf("created workspace %s not found in state", req.Name)
	}
	result.Workspace = *ws
	if err := s.runHook("post_create", lifecycleVars(*ws)); err != nil {
		return result, &HookError{Hook: "post_create", Err: err}
	}
	return result, nil
}

// DeleteRequest is the typed input for a workspace deletion operation.
type DeleteRequest struct {
	Name    string
	Options workspace.RemoveOptions
}

// DeleteResult reports the workspace targeted and whether it was deleted.
type DeleteResult struct {
	Deleted   bool
	Workspace models.Workspace
}

// Delete applies pre_delete policy before invoking destructive workspace logic.
func (s *Service) Delete(req DeleteRequest) (DeleteResult, error) {
	ws, err := s.Store.GetWorkspace(req.Name)
	if err != nil {
		return DeleteResult{}, err
	}
	if ws == nil {
		return DeleteResult{}, fmt.Errorf("workspace %s not found", req.Name)
	}
	result := DeleteResult{Workspace: *ws}
	if err := s.runHook("pre_delete", lifecycleVars(*ws)); err != nil {
		return result, &HookError{Hook: "pre_delete", Err: err}
	}
	req.Options.ExpectedCreatedAt = ws.CreatedAt
	req.Options.ExpectedPath = ws.Path
	if err := s.Workspace.DeleteWithOptions(req.Name, req.Options); err != nil {
		return result, err
	}
	result.Deleted = true
	return result, nil
}

func (s *Service) runHook(name string, vars lifecycle.Vars) error {
	runHook := s.RunHook
	if runHook == nil {
		runHook = lifecycle.Run
	}
	err := runHook(name, vars)
	if err == nil || errors.Is(err, lifecycle.ErrNoHook) {
		return nil
	}
	if lifecycle.ShouldAbort(err) {
		return err
	}
	warn := s.Warn
	if warn == nil {
		warn = console.Warning
	}
	warn(err.Error())
	return nil
}

func expectedWorkspace(req CreateRequest) models.Workspace {
	opts := req.Options
	if req.Preparation != nil {
		opts = req.Preparation.Options.CreateOpts
	}
	ws := models.Workspace{Name: req.Name, Branch: opts.Branch, Source: opts.Source}
	if opts.Cfg != nil {
		ws.Path = filepath.Join(opts.Cfg.WorkspaceDir, req.Name)
	}
	return ws
}

func lifecycleVars(ws models.Workspace) lifecycle.Vars {
	vars := lifecycle.Vars{Name: ws.Name, Path: ws.Path, Branch: ws.Branch}
	if ws.Source != nil {
		vars.SourceURL = ws.Source.URL
		vars.SourceRef = ws.Source.Ref
		vars.SourceTitle = ws.Source.Title
	}
	return vars
}
