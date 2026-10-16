// Package operations owns user-facing workspace operation ordering, including
// global lifecycle hook policy. The workspace package remains lifecycle-free.
package operations

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/lifecycle"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
)

// WorkspaceService contains the lifecycle-free workspace primitives used by
// operation orchestration.
type WorkspaceService interface {
	CreateWithOpts(name string, opts workspace.CreateOpts) error
	DeleteWithOptions(name string, opts workspace.RemoveOptions) error
	AddRepos(name string, repoNames []string, repoMap map[string]string) (int, error)
}

// WorkspaceStore reads the completed workspace used to build lifecycle context.
type WorkspaceStore interface {
	GetWorkspace(name string) (*models.Workspace, error)
}

// ErrHookNotConfigured reports that an operation requires a lifecycle hook that
// is absent (or globally disabled).
var ErrHookNotConfigured = errors.New("required lifecycle hook not configured")

// Service coordinates workspace primitives and global lifecycle hooks.
type Service struct {
	Workspace WorkspaceService
	Store     WorkspaceStore
	RunHook   func(string, lifecycle.Vars) error
	// ResolveHook runs value-returning hooks (resolve_branch).
	ResolveHook func(string, lifecycle.Vars) (string, error)
	Warn        func(string)
}

// NewService creates an operation service with production dependencies.
func NewService() *Service {
	workspaceService := workspace.NewService()
	return &Service{
		Workspace:   workspaceService,
		Store:       workspaceService.State,
		RunHook:     lifecycle.Run,
		ResolveHook: lifecycle.Resolve,
		Warn:        console.Warning,
	}
}

// CreateRequest is the typed input for a workspace creation operation.
type CreateRequest struct {
	Name    string
	Options workspace.CreateOpts
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

// Create resolves the branch name, provisions a workspace, then applies the
// shared post_create policy.
//
// resolve_branch runs exactly once per workspace, here, and never again: the
// resolved branch is stored and reused by add-repo. It is skipped with --track,
// which checks out an existing branch by its real name.
func (s *Service) Create(req CreateRequest) (CreateResult, error) {
	if req.Options.BranchMode != workspace.BranchModeTrack {
		branch, err := s.resolveBranch(req)
		if err != nil {
			return CreateResult{}, &HookError{Hook: "resolve_branch", Err: err}
		}
		req.Options.Branch = branch
	}
	if err := s.Workspace.CreateWithOpts(req.Name, req.Options); err != nil {
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

// AddReposRequest is the typed input for adding repos to an existing workspace.
type AddReposRequest struct {
	Name    string
	Repos   []string
	RepoMap map[string]string
}

// AddRepos adds repos to a workspace, then applies the post_add_repo policy.
// The hook only fires when at least one repo was actually added.
func (s *Service) AddRepos(req AddReposRequest) error {
	added, err := s.Workspace.AddRepos(req.Name, req.Repos, req.RepoMap)
	if err != nil || added == 0 {
		return err
	}
	ws, err := s.Store.GetWorkspace(req.Name)
	if err != nil {
		return fmt.Errorf("loading workspace: %w", err)
	}
	if ws == nil {
		return fmt.Errorf("workspace %s not found in state", req.Name)
	}
	if err := s.runHook("post_add_repo", lifecycleVars(*ws)); err != nil {
		return &HookError{Hook: "post_add_repo", Err: err}
	}
	return nil
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

// CloseRequest is the typed input for the command-specific close operation.
type CloseRequest struct{}

// CloseResult reports whether the configured close hook ran successfully.
type CloseResult struct {
	Closed bool
}

// Close invokes the required on_close hook through operation-level policy.
func (s *Service) Close(CloseRequest) (CloseResult, error) {
	err := s.callHook("on_close", lifecycle.Vars{})
	if errors.Is(err, lifecycle.ErrNoHook) {
		return CloseResult{}, fmt.Errorf("%w: on_close", ErrHookNotConfigured)
	}
	if err != nil {
		return CloseResult{}, &HookError{Hook: "on_close", Err: err}
	}
	return CloseResult{Closed: true}, nil
}

func (s *Service) runHook(name string, vars lifecycle.Vars) error {
	err := s.callHook(name, vars)
	if err == nil || errors.Is(err, lifecycle.ErrNoHook) {
		return nil
	}
	if lifecycle.ShouldAbort(err) {
		return err
	}
	s.warn(err)
	return nil
}

// resolveBranch returns the branch produced by the resolve_branch hook, or the
// typed branch unchanged when no resolver is configured (or hooks are off).
func (s *Service) resolveBranch(req CreateRequest) (string, error) {
	resolve := s.ResolveHook
	if resolve == nil {
		resolve = lifecycle.Resolve
	}
	vars := lifecycle.Vars{Name: req.Name, Branch: req.Options.Branch}
	if src := req.Options.Source; src != nil {
		vars.SourceURL, vars.SourceRef, vars.SourceTitle = src.URL, src.Ref, src.Title
	}
	branch, err := resolve("resolve_branch", vars)
	if errors.Is(err, lifecycle.ErrNoHook) {
		return req.Options.Branch, nil
	}
	if err != nil {
		return "", err
	}
	if err := gitops.CheckBranchName(branch); err != nil {
		return "", fmt.Errorf("resolve_branch hook output: %w", err)
	}
	return branch, nil
}

func (s *Service) callHook(name string, vars lifecycle.Vars) error {
	runHook := s.RunHook
	if runHook == nil {
		runHook = lifecycle.Run
	}
	return runHook(name, vars)
}

func (s *Service) warn(err error) {
	warn := s.Warn
	if warn == nil {
		warn = console.Warning
	}
	warn(err.Error())
}

func expectedWorkspace(req CreateRequest) models.Workspace {
	opts := req.Options
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
