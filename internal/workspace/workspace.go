package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
)

// BranchMode determines how a worktree's branch is provisioned.
type BranchMode int

const (
	// BranchModeCreate creates a new branch from the resolved base branch.
	// This is the default and matches Grove's historical behavior.
	BranchModeCreate BranchMode = iota
	// BranchModeTrack checks out an existing remote branch (e.g. a pull-request
	// head) as a tracking branch instead of creating a new one from base.
	BranchModeTrack
)

// CreateOpts carries the inputs for creating a workspace. It groups the original
// positional Create parameters and adds optional branch-mode and provenance.
type CreateOpts struct {
	Branch  string
	Repos   []string
	RepoMap map[string]string // repo name → source path
	Cfg     *models.Config

	// BranchMode is the mode applied to the repos selected by TrackBranchRepo.
	// When TrackBranchRepo is empty, BranchMode applies to every repo; when set,
	// it applies only to that one repo and all others use BranchModeCreate.
	//
	// A PR URL identifies exactly one repo+branch, so a resolver names that repo
	// in TrackBranchRepo — sibling repos added to the same workspace then still
	// get fresh branches from base rather than coincidentally tracking a remote
	// branch of the same name. Track mode always falls back to create mode for
	// any repo where the remote branch does not exist, so a blanket
	// (empty-TrackBranchRepo) track is safe too.
	BranchMode      BranchMode
	TrackBranchRepo string

	// Source, when set, is persisted on the workspace as provenance (e.g. the
	// GitHub PR / Notion page / Slack thread it was seeded from). Opaque to core.
	Source *models.WorkspaceSource
}

// PreparationOpts configures a workspace whose exact bases are already resolved.
type PreparationOpts struct {
	CreateOpts
	BaseCommits map[string]string
}

// PreparationError preserves the original preparation failure and any cleanup failure.
type PreparationError struct {
	Cause      error
	CleanupErr error
}

func (e *PreparationError) Error() string {
	if e.CleanupErr != nil {
		return fmt.Sprintf("preparing workspace: %s; cleanup failed: %s", e.Cause, e.CleanupErr)
	}
	return fmt.Sprintf("preparing workspace: %s", e.Cause)
}

func (e *PreparationError) Unwrap() error { return e.Cause }

// Create creates a new workspace with worktrees for the given repos.
// repoMap is name→source_path. It preserves the historical positional signature
// by delegating to CreateWithOpts.
func (s *Service) Create(name, branch string, repoNames []string, repoMap map[string]string, cfg *models.Config) error {
	return s.CreateWithOpts(name, CreateOpts{
		Branch:  branch,
		Repos:   repoNames,
		RepoMap: repoMap,
		Cfg:     cfg,
	})
}

// CreateWithOpts creates a new workspace from the given options. It is the full
// implementation behind Create, additionally supporting per-repo branch tracking
// (BranchMode/TrackBranchRepo) and a persisted Source link.
func (s *Service) CreateWithOpts(name string, opts CreateOpts) error {
	var ws models.Workspace
	if err := s.State.WithLock(func() error {
		var err error
		ws, err = s.createWithOptsLocked(name, opts)
		return err
	}); err != nil {
		return err
	}

	// Setup commands are user-owned and may invoke gw, so run them after the
	// workspace is committed and the cross-process mutation lock is released.
	if hasSetupHooks(ws) {
		console.Infof("running setup hooks...")
	}
	s.runSetupHooks(ws)
	s.finishCreate(ws)
	return nil
}

// CreateWithPreparation provisions exact Recipe bases, runs prepare outside the
// mutation lock, and rolls back only resources owned by this invocation.
func (s *Service) CreateWithPreparation(name string, opts PreparationOpts, prepare func(models.Workspace) error) error {
	var created creationResult
	if err := s.State.WithLock(func() error {
		var err error
		created, err = s.createWithOptsLockedDetailed(name, opts.CreateOpts, creationSettings{
			skipFetch:   true,
			baseCommits: opts.BaseCommits,
		})
		return err
	}); err != nil {
		return err
	}

	if err := prepare(created.workspace); err != nil {
		cleanupErr := s.rollbackPreparation(created)
		return &PreparationError{Cause: err, CleanupErr: cleanupErr}
	}
	if err := s.verifyPreparationState(created.workspace); err != nil {
		return &PreparationError{Cause: err, CleanupErr: err}
	}
	if err := verifyProvisionedWorktreeIdentity(created.provisioned); err != nil {
		return &PreparationError{Cause: err, CleanupErr: err}
	}
	s.finishCreate(created.workspace)
	return nil
}

func (s *Service) finishCreate(ws models.Workspace) {
	s.Stats.RecordCreated(ws)
	logging.Info("workspace %q created at %s", ws.Name, ws.Path)
	console.Successf("Workspace %s created at %s", ws.Name, ws.Path)
	if cdFile := os.Getenv("GROVE_CD_FILE"); cdFile != "" {
		_ = os.WriteFile(cdFile, []byte(ws.Path), 0o644)
	}
}

type creationSettings struct {
	skipFetch   bool
	baseCommits map[string]string
}

type creationResult struct {
	workspace   models.Workspace
	provisioned []provisionedRepo
}

func (s *Service) createWithOptsLocked(name string, opts CreateOpts) (models.Workspace, error) {
	created, err := s.createWithOptsLockedDetailed(name, opts, creationSettings{})
	return created.workspace, err
}

func (s *Service) createWithOptsLockedDetailed(name string, opts CreateOpts, settings creationSettings) (creationResult, error) {
	branch := opts.Branch
	repoNames := opts.Repos
	repoMap := opts.RepoMap
	cfg := opts.Cfg

	existing, err := s.State.GetWorkspace(name)
	if err != nil {
		return creationResult{}, err
	}
	if existing != nil {
		return creationResult{}, fmt.Errorf("workspace %s already exists", name)
	}

	logging.Info("creating workspace %q (branch=%s, repos=%v)", name, branch, repoNames)

	if err := os.MkdirAll(cfg.WorkspaceDir, 0o755); err != nil {
		return creationResult{}, fmt.Errorf("creating workspace parent: %w", err)
	}
	wsPath := filepath.Join(cfg.WorkspaceDir, name)
	if err := os.Mkdir(wsPath, 0o755); err != nil {
		return creationResult{}, fmt.Errorf("creating workspace dir: %w", err)
	}

	ws := models.NewWorkspace(name, wsPath, branch)
	ws.Source = opts.Source

	// Validate all repo names and exact bases before provisioning.
	sourcePaths := make([]string, len(repoNames))
	baseCommits := make([]string, len(repoNames))
	for i, repoName := range repoNames {
		sourcePath, ok := repoMap[repoName]
		if !ok {
			return creationResult{}, errors.Join(
				fmt.Errorf("repo %s not found", repoName),
				removeEmptyDir(wsPath),
			)
		}
		sourcePaths[i] = sourcePath
		if settings.baseCommits != nil {
			base, ok := settings.baseCommits[repoName]
			if !ok || base == "" {
				return creationResult{}, errors.Join(
					fmt.Errorf("repo %s has no resolved base commit", repoName),
					removeEmptyDir(wsPath),
				)
			}
			baseCommits[i] = base
		}
	}

	// Phase 1: parallel fetch (the slow network part). Recipe creation resolves
	// and fetches exact commits before acquiring the mutation lock.
	if !settings.skipFetch {
		console.Infof("fetching %d repos...", len(repoNames))
		var fetchWg sync.WaitGroup
		for i, repoName := range repoNames {
			fetchWg.Add(1)
			go func(source, name string) {
				defer fetchWg.Done()
				if err := gitops.Fetch(source); err != nil {
					console.Warningf("  %s: fetch failed, using local state", name)
				}
			}(sourcePaths[i], repoName)
		}
		fetchWg.Wait()
	}

	// Phase 2: sequential worktree creation (for rollback safety)
	var created []provisionedRepo
	for i, repoName := range repoNames {
		console.Infof("[%d/%d] %s", i+1, len(repoNames), repoName)
		mode := opts.BranchMode
		if opts.TrackBranchRepo != "" && repoName != opts.TrackBranchRepo {
			mode = BranchModeCreate
		}
		provisioned, err := provisionWorktreeNoFetch(sourcePaths[i], repoName, wsPath, branch, mode, baseCommits[i])
		if err != nil {
			logging.Error("workspace creation failed for %q — rolling back", name)
			return creationResult{}, errors.Join(
				fmt.Errorf("provisioning %s: %w", repoName, err),
				s.rollback(created),
				removeEmptyDir(wsPath),
			)
		}
		created = append(created, *provisioned)
		ws.Repos = append(ws.Repos, provisioned.worktree)
	}

	if err := s.State.AddWorkspace(ws); err != nil {
		return creationResult{}, errors.Join(err, s.rollback(created), removeEmptyDir(wsPath))
	}

	writeMCPConfig(ws)
	return creationResult{workspace: ws, provisioned: created}, nil
}

type provisionedRepo struct {
	worktree      models.RepoWorktree
	branchCreated bool
}

func provisionWorktree(sourcePath, repoName, wsPath, branch string) (*provisionedRepo, error) {
	_ = gitops.Fetch(sourcePath)
	return provisionWorktreeNoFetch(sourcePath, repoName, wsPath, branch, BranchModeCreate, "")
}

func provisionWorktreeNoFetch(sourcePath, repoName, wsPath, branch string, mode BranchMode, exactBase string) (*provisionedRepo, error) {
	wtPath := filepath.Join(wsPath, repoName)

	hasWT, _ := gitops.WorktreeHasBranch(sourcePath, branch)
	if hasWT {
		return nil, fmt.Errorf("branch %s already has a worktree in %s", branch, repoName)
	}

	// Tracking creates the local branch as part of git worktree add.
	if mode == BranchModeTrack && !gitops.BranchExists(sourcePath, branch) {
		if gitops.RemoteBranchExists(sourcePath, branch) {
			logging.Info("tracking existing remote branch %q in %s", branch, repoName)
			if err := gitops.WorktreeAddTracking(sourcePath, wtPath, branch); err != nil {
				var cleanupErr error
				if gitops.BranchExists(sourcePath, branch) {
					if err := gitops.DeleteBranch(sourcePath, branch, true); err != nil {
						cleanupErr = fmt.Errorf("rolling back branch: %w", err)
					}
				}
				return nil, errors.Join(fmt.Errorf("adding tracking worktree: %w", err), cleanupErr)
			}
			return &provisionedRepo{
				worktree: models.RepoWorktree{
					RepoName:     repoName,
					SourceRepo:   sourcePath,
					WorktreePath: wtPath,
					Branch:       branch,
				},
				branchCreated: true,
			}, nil
		}
		console.Warningf("%s: remote branch %s not found — creating a new branch from base instead", repoName, branch)
	}

	branchCreated, err := ensureWorkspaceBranch(sourcePath, repoName, branch, exactBase)
	if err != nil {
		return nil, err
	}

	if err := gitops.WorktreeAdd(sourcePath, wtPath, branch); err != nil {
		var cleanupErr error
		if branchCreated {
			if err := gitops.DeleteBranch(sourcePath, branch, true); err != nil {
				cleanupErr = fmt.Errorf("rolling back branch: %w", err)
			}
		}
		return nil, errors.Join(fmt.Errorf("adding worktree: %w", err), cleanupErr)
	}

	return &provisionedRepo{
		worktree: models.RepoWorktree{
			RepoName:       repoName,
			SourceRepo:     sourcePath,
			WorktreePath:   wtPath,
			Branch:         branch,
			PreserveBranch: exactBase != "" && !branchCreated,
		},
		branchCreated: branchCreated,
	}, nil
}

func ensureWorkspaceBranch(sourcePath, repoName, branch, exactBase string) (bool, error) {
	if gitops.BranchExists(sourcePath, branch) {
		if exactBase == "" {
			return false, nil
		}
		branchCommit, err := gitops.LocalBranchCommit(sourcePath, branch)
		if err != nil || branchCommit != exactBase {
			return false, fmt.Errorf("branch %s already exists at a different commit", branch)
		}
		return false, nil
	}

	base := exactBase
	if base == "" {
		var err error
		base, err = gitops.ResolveBaseBranch(sourcePath)
		if err != nil {
			base = "HEAD"
		}
	}
	logging.Info("creating branch %q in %s from %s", branch, repoName, base)
	if err := gitops.CreateBranch(sourcePath, branch, base); err != nil {
		if exactBase != "" {
			return false, fmt.Errorf("creating branch from exact base %s: %w", exactBase, err)
		}
		plainBase := strings.TrimPrefix(base, "origin/")
		if err2 := gitops.CreateBranch(sourcePath, branch, plainBase); err2 != nil {
			if err3 := gitops.CreateBranch(sourcePath, branch, "HEAD"); err3 != nil {
				return false, fmt.Errorf("creating branch: %w", err)
			}
		}
	}
	return true, nil
}

func (s *Service) rollback(repos []provisionedRepo) error {
	var errs []error
	for i := len(repos) - 1; i >= 0; i-- {
		repo := repos[i]
		if err := s.removeWorktree(repo.worktree.SourceRepo, repo.worktree.WorktreePath, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: rolling back worktree: %w", repo.worktree.RepoName, err))
			continue
		}
		if repo.branchCreated {
			if err := gitops.DeleteBranch(repo.worktree.SourceRepo, repo.worktree.Branch, true); err != nil {
				errs = append(errs, fmt.Errorf("%s: rolling back branch: %w", repo.worktree.RepoName, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) verifyPreparationState(expected models.Workspace) error {
	return s.State.WithLock(func() error {
		current, err := s.State.GetWorkspace(expected.Name)
		if err != nil {
			return err
		}
		if current == nil || !reflect.DeepEqual(*current, expected) {
			return fmt.Errorf("workspace state changed during preparation")
		}
		return nil
	})
}

func (s *Service) rollbackPreparation(created creationResult) error {
	return s.State.WithLock(func() error {
		current, err := s.State.GetWorkspace(created.workspace.Name)
		if err != nil {
			return err
		}
		if current == nil || !reflect.DeepEqual(*current, created.workspace) {
			return fmt.Errorf("workspace state changed during preparation; refusing automatic cleanup")
		}
		if err := verifyProvisionedWorktreeIdentity(created.provisioned); err != nil {
			return err
		}

		failedWorktrees := make(map[string]bool)
		var cleanupErrs []error
		for i := len(created.provisioned) - 1; i >= 0; i-- {
			repo := created.provisioned[i]
			if err := s.removeWorktree(repo.worktree.SourceRepo, repo.worktree.WorktreePath, true); err != nil {
				failedWorktrees[repo.worktree.WorktreePath] = true
				cleanupErrs = append(cleanupErrs, fmt.Errorf("%s: rolling back worktree: %w", repo.worktree.RepoName, err))
				continue
			}
			if repo.branchCreated {
				if err := gitops.DeleteBranch(repo.worktree.SourceRepo, repo.worktree.Branch, true); err != nil {
					cleanupErrs = append(cleanupErrs, fmt.Errorf("%s: rolling back branch: %w", repo.worktree.RepoName, err))
				}
			}
		}

		if len(failedWorktrees) > 0 {
			remaining := make([]models.RepoWorktree, 0, len(failedWorktrees))
			for _, repo := range current.Repos {
				if failedWorktrees[repo.WorktreePath] {
					remaining = append(remaining, repo)
				}
			}
			current.Repos = remaining
			if err := s.State.UpdateWorkspace(*current); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("updating state after cleanup failure: %w", err))
			}
			return errors.Join(cleanupErrs...)
		}

		removeMCPConfig(*current)
		if err := os.Remove(current.Path); err != nil && !os.IsNotExist(err) {
			current.Repos = nil
			cleanupErrs = append(cleanupErrs, fmt.Errorf("removing workspace root %s: %w", current.Path, err))
			if stateErr := s.State.UpdateWorkspace(*current); stateErr != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("updating state after root cleanup failure: %w", stateErr))
			}
			return errors.Join(cleanupErrs...)
		}
		if err := s.State.RemoveWorkspace(current.Name); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
		return errors.Join(cleanupErrs...)
	})
}

func verifyProvisionedWorktreeIdentity(repos []provisionedRepo) error {
	var errs []error
	for _, provisioned := range repos {
		repo := provisioned.worktree
		entries, err := gitops.WorktreeList(repo.SourceRepo)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: reading worktree registration: %w", repo.RepoName, err))
			continue
		}
		expectedPath := canonicalPath(repo.WorktreePath)
		registered := false
		for _, entry := range entries {
			if canonicalPath(entry.Path) == expectedPath && entry.Branch == repo.Branch {
				registered = true
				break
			}
		}
		if !registered {
			errs = append(errs, fmt.Errorf("%s: worktree identity changed; refusing automatic cleanup", repo.RepoName))
			continue
		}
		branch, err := gitops.CurrentBranch(repo.WorktreePath)
		if err != nil || branch != repo.Branch {
			errs = append(errs, fmt.Errorf("%s: current branch changed; refusing automatic cleanup", repo.RepoName))
		}
	}
	return errors.Join(errs...)
}

func removeEmptyDir(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing empty workspace root %s: %w", path, err)
	}
	return nil
}

func hasSetupHooks(ws models.Workspace) bool {
	for _, r := range ws.Repos {
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg != nil && len(groveCfg.Setup) > 0 {
			return true
		}
	}
	return false
}

func (s *Service) runSetupHooks(ws models.Workspace) {
	var wg sync.WaitGroup
	for _, r := range ws.Repos {
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg == nil || len(groveCfg.Setup) == 0 {
			continue
		}
		wg.Add(1)
		go func(repo models.RepoWorktree, cmds []string) {
			defer wg.Done()
			for _, cmdStr := range cmds {
				if err := s.RunCmd(repo.WorktreePath, cmdStr); err != nil {
					console.Warningf("setup hook failed in %s: %s", repo.RepoName, err)
				}
			}
		}(r, []string(groveCfg.Setup))
	}
	wg.Wait()
}

// mcpServerEntry returns the grove MCP server config.
func mcpServerEntry(wsName string) models.MCPServer {
	return models.MCPServer{
		Command: "gw",
		Args:    []string{"mcp-serve", "--workspace", wsName},
	}
}

// mergeMCPConfig reads existing .mcp.json, adds/updates the grove entry, writes back.
func mergeMCPConfig(path string, wsName string) {
	var existing map[string]any

	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	servers, ok := existing["mcpServers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
	}
	servers["grove"] = mcpServerEntry(wsName)
	existing["mcpServers"] = servers

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

func writeMCPConfig(ws models.Workspace) {
	mergeMCPConfig(filepath.Join(ws.Path, ".mcp.json"), ws.Name)
}

// removeMCPConfig removes the grove entry from the workspace's .mcp.json.
func removeMCPConfig(ws models.Workspace) {
	path := filepath.Join(ws.Path, ".mcp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var existing map[string]any
	if err := json.Unmarshal(data, &existing); err != nil {
		return
	}
	servers, ok := existing["mcpServers"].(map[string]any)
	if !ok {
		return
	}
	delete(servers, "grove")
	if len(servers) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logging.Warn("could not remove %s: %s", path, err)
		}
		return
	}
	existing["mcpServers"] = servers
	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		logging.Warn("could not marshal %s: %s", path, err)
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		logging.Warn("could not update %s: %s", path, err)
	}
}

// RemoveOptions controls destructive worktree cleanup.
type RemoveOptions struct {
	Force bool
}

// Delete removes a workspace using safe defaults.
func (s *Service) Delete(name string) error {
	return s.DeleteWithOptions(name, RemoveOptions{})
}

// DeleteWithOptions removes a workspace and its worktrees.
func (s *Service) DeleteWithOptions(name string, opts RemoveOptions) error {
	ws, err := s.State.GetWorkspace(name)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %s not found", name)
	}

	if err := preflightRemovals(ws.Repos, opts.Force); err != nil {
		return err
	}

	// Teardown commands are user-owned and may invoke gw. Run them before the
	// mutation lock, then reload and preflight state again while locked.
	s.runTeardownHooks(ws.Repos)

	var deleted *models.Workspace
	if err := s.State.WithLock(func() error {
		var err error
		deleted, err = s.deleteLocked(name, opts)
		return err
	}); err != nil {
		return err
	}

	s.Stats.RecordDeleted(*deleted)
	logging.Info("workspace %q deleted", name)
	console.Successf("Workspace %s deleted", name)
	return nil
}

func (s *Service) deleteLocked(name string, opts RemoveOptions) (*models.Workspace, error) {
	ws, err := s.State.GetWorkspace(name)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace %s not found", name)
	}
	if err := preflightRemovals(ws.Repos, opts.Force); err != nil {
		return nil, err
	}

	logging.Info("deleting workspace %q", name)
	original := *ws
	remaining := make([]models.RepoWorktree, 0, len(ws.Repos))
	var cleanupErrs []error

	for _, repo := range ws.Repos {
		if err := s.removeWorktree(repo.SourceRepo, repo.WorktreePath, opts.Force); err != nil {
			remaining = append(remaining, repo)
			cleanupErrs = append(cleanupErrs, fmt.Errorf("%s: removing worktree: %w", repo.RepoName, err))
			continue
		}
		s.deleteBranch(repo, opts.Force)
	}

	if len(remaining) > 0 {
		ws.Repos = remaining
		if err := s.State.UpdateWorkspace(*ws); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("updating state: %w", err))
		}
		return nil, errors.Join(cleanupErrs...)
	}

	removeMCPConfig(*ws)
	if err := os.Remove(ws.Path); err != nil && !os.IsNotExist(err) {
		ws.Repos = nil
		cleanupErrs = append(cleanupErrs, fmt.Errorf("removing workspace root %s: %w", ws.Path, err))
		if stateErr := s.State.UpdateWorkspace(*ws); stateErr != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("updating state: %w", stateErr))
		}
		return nil, errors.Join(cleanupErrs...)
	}

	if err := s.State.RemoveWorkspace(name); err != nil {
		return nil, err
	}
	return &original, nil
}

func (s *Service) runTeardownHooks(repos []models.RepoWorktree) {
	for _, repo := range repos {
		groveCfg, _ := gitops.ReadGroveConfig(repo.SourceRepo)
		if groveCfg != nil && groveCfg.Teardown != "" {
			s.RunCmdSilent(repo.WorktreePath, groveCfg.Teardown)
		}
	}
}

func (s *Service) removeWorktree(repo, path string, force bool) error {
	if s.RemoveWorktree != nil {
		return s.RemoveWorktree(repo, path, force)
	}
	return gitops.WorktreeRemove(repo, path, force)
}

func (s *Service) deleteBranch(repo models.RepoWorktree, force bool) {
	if repo.PreserveBranch {
		logging.Info("preserving pre-existing branch %q in %s", repo.Branch, repo.RepoName)
		return
	}
	if err := gitops.DeleteBranch(repo.SourceRepo, repo.Branch, force); err != nil {
		logging.Warn("failed to delete branch %q in %s: %s", repo.Branch, repo.RepoName, err)
		console.Warningf("%s: failed to delete branch %s: %s", repo.RepoName, repo.Branch, err)
		return
	}
	logging.Info("deleted branch %q in %s", repo.Branch, repo.RepoName)
}

func preflightRemovals(repos []models.RepoWorktree, force bool) error {
	if force {
		return nil
	}
	var errs []error
	for _, repo := range repos {
		if err := preflightRemoval(repo); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", repo.RepoName, err))
		}
	}
	return errors.Join(errs...)
}

func preflightRemoval(repo models.RepoWorktree) error {
	entries, err := gitops.WorktreeList(repo.SourceRepo)
	if err != nil {
		return fmt.Errorf("reading worktree registration: %w", err)
	}

	expectedPath := canonicalPath(repo.WorktreePath)
	registered := false
	for _, entry := range entries {
		if canonicalPath(entry.Path) != expectedPath {
			continue
		}
		registered = true
		if entry.Branch != repo.Branch {
			return fmt.Errorf("unexpected branch %q at %s (expected %q)", entry.Branch, repo.WorktreePath, repo.Branch)
		}
		break
	}
	if !registered {
		return fmt.Errorf("worktree path is not registered: %s", repo.WorktreePath)
	}

	branch, err := gitops.CurrentBranch(repo.WorktreePath)
	if err != nil {
		return fmt.Errorf("reading current branch: %w", err)
	}
	if branch != repo.Branch {
		return fmt.Errorf("unexpected current branch %q (expected %q)", branch, repo.Branch)
	}

	status, err := gitops.RepoStatus(repo.WorktreePath)
	if err != nil {
		return fmt.Errorf("reading worktree status: %w", err)
	}
	if status != "" {
		return fmt.Errorf("dirty worktree; commit, stash, or use --force")
	}
	return nil
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

// AddRepos adds repos to an existing workspace.
func (s *Service) AddRepos(wsName string, repoNames []string, repoMap map[string]string) error {
	var added []models.RepoWorktree
	if err := s.State.WithLock(func() error {
		var err error
		added, err = s.addReposLocked(wsName, repoNames, repoMap)
		return err
	}); err != nil {
		return err
	}

	if len(added) == 0 {
		return nil
	}

	// As with create, setup runs after the state mutation lock is released.
	s.runSetupHooks(models.Workspace{Repos: added})
	logging.Info("added %d repo(s) to workspace %q", len(added), wsName)
	console.Successf("Added %d repo(s) to %s", len(added), wsName)
	return nil
}

func (s *Service) addReposLocked(wsName string, repoNames []string, repoMap map[string]string) ([]models.RepoWorktree, error) {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace %s not found", wsName)
	}

	existing := make(map[string]bool)
	for _, r := range ws.Repos {
		existing[r.RepoName] = true
	}

	var toAdd []string
	for _, name := range repoNames {
		if !existing[name] {
			toAdd = append(toAdd, name)
		}
	}

	if len(toAdd) == 0 {
		console.Info("All repos already in workspace")
		return nil, nil
	}

	sourcePaths := make([]string, len(toAdd))
	for i, repoName := range toAdd {
		sourcePath, ok := repoMap[repoName]
		if !ok {
			return nil, fmt.Errorf("repo %s not found", repoName)
		}
		sourcePaths[i] = sourcePath
	}

	created := make([]provisionedRepo, 0, len(toAdd))
	for i, repoName := range toAdd {
		provisioned, err := provisionWorktree(sourcePaths[i], repoName, ws.Path, ws.Branch)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("adding %s: %w", repoName, err), s.rollback(created))
		}
		created = append(created, *provisioned)
		ws.Repos = append(ws.Repos, provisioned.worktree)
	}

	if err := s.State.UpdateWorkspace(*ws); err != nil {
		return nil, errors.Join(err, s.rollback(created))
	}

	added := make([]models.RepoWorktree, 0, len(created))
	for _, repo := range created {
		added = append(added, repo.worktree)
	}
	return added, nil
}

// RemoveRepos removes repos from a workspace using safe defaults.
func (s *Service) RemoveRepos(wsName string, repoNames []string) error {
	return s.RemoveReposWithOptions(wsName, repoNames, RemoveOptions{})
}

// RemoveReposWithOptions removes selected repos from a workspace.
func (s *Service) RemoveReposWithOptions(wsName string, repoNames []string, opts RemoveOptions) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %s not found", wsName)
	}

	selected := selectRepos(ws, repoNames)
	if err := preflightRemovals(selected, opts.Force); err != nil {
		return err
	}
	s.runTeardownHooks(selected)

	var removed int
	if err := s.State.WithLock(func() error {
		var err error
		removed, err = s.removeReposLocked(wsName, repoNames, opts)
		return err
	}); err != nil {
		return err
	}
	if removed == 0 {
		return nil
	}

	logging.Info("removed %d repo(s) from workspace %q", removed, wsName)
	console.Successf("Removed %d repo(s) from %s", removed, wsName)
	return nil
}

func (s *Service) removeReposLocked(wsName string, repoNames []string, opts RemoveOptions) (int, error) {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return 0, err
	}
	if ws == nil {
		return 0, fmt.Errorf("workspace %s not found", wsName)
	}

	items := selectRepos(ws, repoNames)
	if err := preflightRemovals(items, opts.Force); err != nil {
		return 0, err
	}

	removed := 0
	var cleanupErrs []error
	for _, repo := range items {
		if err := s.removeWorktree(repo.SourceRepo, repo.WorktreePath, opts.Force); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("%s: removing worktree: %w", repo.RepoName, err))
			continue
		}
		s.deleteBranch(repo, opts.Force)
		ws.RemoveRepo(repo.RepoName)
		removed++
	}

	if removed > 0 {
		if err := s.State.UpdateWorkspace(*ws); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("updating state: %w", err))
		}
	}
	return removed, errors.Join(cleanupErrs...)
}

func selectRepos(ws *models.Workspace, names []string) []models.RepoWorktree {
	selected := make([]models.RepoWorktree, 0, len(names))
	for _, name := range names {
		if repo := ws.FindRepo(name); repo != nil {
			selected = append(selected, *repo)
		}
	}
	return selected
}

// syncOneRepo syncs a single repo.
func (s *Service) syncOneRepo(r models.RepoWorktree) {
	if err := gitops.Fetch(r.SourceRepo); err != nil {
		console.Warningf("%s: fetch failed, using local state: %s", r.RepoName, err)
	}

	groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)

	status, err := gitops.RepoStatus(r.WorktreePath)
	if err != nil {
		console.Warningf("%s: status check failed: %s", r.RepoName, err)
		return
	}
	if status != "" {
		console.Warningf("%s: skipping (dirty working tree)", r.RepoName)
		return
	}

	upstream, err := gitops.ResolveBaseBranch(r.SourceRepo)
	if err != nil {
		console.Warningf("%s: could not determine base branch: %s", r.RepoName, err)
		return
	}

	_, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, upstream)
	if err != nil {
		console.Warningf("%s: cannot determine ahead/behind: %s", r.RepoName, err)
		return
	}

	if behind == 0 {
		console.Infof("%s: ✓ up to date", r.RepoName)
		return
	}

	if groveCfg != nil && groveCfg.PreSync != "" {
		s.RunCmdSilent(r.WorktreePath, groveCfg.PreSync)
	}

	if err := gitops.RebaseOnto(r.WorktreePath, upstream); err != nil {
		console.Errorf("%s: rebase failed: %s", r.RepoName, err)
		gitops.RebaseAbort(r.WorktreePath)
		return
	}

	console.Successf("%s: rebased (%d commits)", r.RepoName, behind)

	if groveCfg != nil && groveCfg.PostSync != "" {
		s.RunCmdSilent(r.WorktreePath, groveCfg.PostSync)
	}
}

// Sync rebases workspace repos onto their base branches.
func (s *Service) Sync(wsName string) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %s not found", wsName)
	}

	logging.Info("syncing workspace %q", wsName)

	var wg sync.WaitGroup
	for _, r := range ws.Repos {
		wg.Add(1)
		go func(repo models.RepoWorktree) {
			defer wg.Done()
			s.syncOneRepo(repo)
		}(r)
	}
	wg.Wait()

	return nil
}

type repoStatusResult struct {
	Repo   string         `json:"repo"`
	Branch string         `json:"branch"`
	Status string         `json:"status"`
	Ahead  string         `json:"ahead"`
	Behind string         `json:"behind"`
	PR     *gitops.PRInfo `json:"pr,omitempty"`
}

func collectRepoStatus(r models.RepoWorktree) repoStatusResult {
	rs := repoStatusResult{
		Repo:   r.RepoName,
		Branch: r.Branch,
	}
	if branch, err := gitops.CurrentBranch(r.WorktreePath); err == nil {
		if branch == "" {
			rs.Branch = "(detached)"
		} else {
			rs.Branch = branch
		}
	}
	status, err := gitops.RepoStatus(r.WorktreePath)
	if err != nil {
		rs.Status = "error: " + err.Error()
		rs.Ahead = "-"
		rs.Behind = "-"
		return rs
	}
	if status == "" {
		rs.Status = "clean"
	} else {
		rs.Status = status
	}

	upstream, _ := gitops.ResolveBaseBranch(r.SourceRepo)
	if upstream == "" {
		upstream = "origin/main"
	}
	ahead, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, upstream)
	if err == nil {
		rs.Ahead = fmt.Sprintf("%d", ahead)
		rs.Behind = fmt.Sprintf("%d", behind)
	} else {
		rs.Ahead = "-"
		rs.Behind = "-"
	}

	return rs
}

func formatPR(pr *gitops.PRInfo) string {
	if pr == nil {
		return "-"
	}
	switch pr.State {
	case "MERGED":
		return fmt.Sprintf("#%d merged", pr.Number)
	case "CLOSED":
		return fmt.Sprintf("#%d closed", pr.Number)
	case "OPEN":
		switch pr.ReviewDecision {
		case "APPROVED":
			return fmt.Sprintf("#%d ✓", pr.Number)
		case "CHANGES_REQUESTED":
			return fmt.Sprintf("#%d ✗", pr.Number)
		default:
			return fmt.Sprintf("#%d open", pr.Number)
		}
	default:
		return fmt.Sprintf("#%d %s", pr.Number, pr.State)
	}
}

// formatSourceLine renders a workspace's source provenance as a single line for
// status output, or "" if there is no source. e.g.
// "Source: github 1172 — Surface data source status  (https://github.com/...)".
func formatSourceLine(src *models.WorkspaceSource) string {
	if src == nil {
		return ""
	}
	label := src.Provider
	if label == "" {
		label = "source"
	}
	if src.Ref != "" {
		label += " " + src.Ref
	}
	if src.Title != "" {
		label += " — " + src.Title
	}
	if src.URL != "" {
		label += "  (" + src.URL + ")"
	}
	return "Source: " + label
}

// StatusOptions controls status output.
type StatusOptions struct {
	JSON    bool
	Verbose bool
	PR      bool
}

// Status displays git status for a workspace.
func (s *Service) Status(wsName string, opts StatusOptions) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %s not found", wsName)
	}

	results := s.fetchStatusResults(ws.Repos, opts.PR)

	if opts.JSON {
		return s.printStatusJSON(ws, results)
	}

	s.printStatusTable(ws, results, opts)
	s.printVerboseStatus(results, opts)
	return nil
}

func (s *Service) fetchStatusResults(repos []models.RepoWorktree, withPR bool) []repoStatusResult {
	results := make([]repoStatusResult, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(idx int, repo models.RepoWorktree) {
			defer wg.Done()
			results[idx] = collectRepoStatus(repo)
			if withPR {
				results[idx].PR = gitops.PRStatus(repo.WorktreePath)
			}
		}(i, r)
	}
	wg.Wait()
	return results
}

func (s *Service) printStatusJSON(ws *models.Workspace, results []repoStatusResult) error {
	type wsStatus struct {
		Workspace string                  `json:"workspace"`
		Path      string                  `json:"path"`
		Source    *models.WorkspaceSource `json:"source,omitempty"`
		Repos     []repoStatusResult      `json:"repos"`
	}
	data, _ := json.MarshalIndent(wsStatus{
		Workspace: ws.Name,
		Path:      ws.Path,
		Source:    ws.Source,
		Repos:     results,
	}, "", "  ")
	fmt.Println(string(data))
	return nil
}

func (s *Service) printStatusTable(ws *models.Workspace, results []repoStatusResult, opts StatusOptions) {
	fmt.Fprintf(os.Stdout, "Workspace: %s  (%s)\n", ws.Name, ws.Path)
	if line := formatSourceLine(ws.Source); line != "" {
		fmt.Fprintf(os.Stdout, "%s\n", line)
	}
	fmt.Fprintln(os.Stdout)

	headers := []string{"Repo", "Branch", "↑↓", "Status"}
	if opts.PR {
		headers = []string{"Repo", "Branch", "↑↓", "PR", "Status"}
	}
	table := console.NewTable(os.Stdout, headers)

	for _, rs := range results {
		table.AddRow(statusRow(rs, opts.PR))
	}
	table.Render()
}

func statusRow(rs repoStatusResult, withPR bool) []string {
	upDown := formatUpDown(rs.Ahead, rs.Behind)
	statusStr := formatStatus(rs.Status)
	if withPR {
		prStr := "-"
		if rs.PR != nil {
			prStr = formatPR(rs.PR)
		}
		return []string{rs.Repo, rs.Branch, upDown, prStr, statusStr}
	}
	return []string{rs.Repo, rs.Branch, upDown, statusStr}
}

func formatUpDown(ahead, behind string) string {
	if ahead != "-" && behind != "-" && ahead != "" && behind != "" {
		return fmt.Sprintf("%s↑ %s↓", ahead, behind)
	}
	return "-"
}

func formatStatus(status string) string {
	if status == "clean" || status == "" || strings.HasPrefix(status, "error:") {
		return status
	}
	lines := strings.Count(status, "\n") + 1
	return fmt.Sprintf("%d changed", lines)
}

func (s *Service) printVerboseStatus(results []repoStatusResult, opts StatusOptions) {
	if !opts.Verbose {
		return
	}
	for _, rs := range results {
		if rs.Status != "clean" && rs.Status != "" && !strings.HasPrefix(rs.Status, "error:") {
			fmt.Fprintf(os.Stderr, "\n%s:\n%s\n", rs.Repo, rs.Status)
		}
	}
}

// WorkspaceSummary holds summary info for list --status.
type WorkspaceSummary struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Repos  int    `json:"repos"`
	Status string `json:"status"`
	Path   string `json:"path"`
}

// AllWorkspacesSummary returns a status summary for all workspaces.
func (s *Service) AllWorkspacesSummary() ([]WorkspaceSummary, error) {
	workspaces, err := s.State.Load()
	if err != nil {
		return nil, err
	}

	if len(workspaces) == 0 {
		return []WorkspaceSummary{}, nil
	}

	results := make([]WorkspaceSummary, len(workspaces))
	var wg sync.WaitGroup
	for i, ws := range workspaces {
		wg.Add(1)
		go func(idx int, w models.Workspace) {
			defer wg.Done()
			summary := WorkspaceSummary{
				Name:   w.Name,
				Branch: w.Branch,
				Repos:  len(w.Repos),
				Path:   w.Path,
			}

			clean, dirty, errCount := 0, 0, 0
			for _, r := range w.Repos {
				status, err := gitops.RepoStatus(r.WorktreePath)
				if err != nil {
					errCount++
				} else if status == "" {
					clean++
				} else {
					dirty++
				}
			}

			parts := []string{}
			if clean > 0 {
				parts = append(parts, fmt.Sprintf("%d clean", clean))
			}
			if dirty > 0 {
				parts = append(parts, fmt.Sprintf("%d modified", dirty))
			}
			if errCount > 0 {
				parts = append(parts, fmt.Sprintf("%d error", errCount))
			}
			summary.Status = strings.Join(parts, ", ")
			if summary.Status == "" {
				summary.Status = "empty"
			}

			results[idx] = summary
		}(i, ws)
	}
	wg.Wait()

	return results, nil
}

// Doctor checks workspace health and returns issues.
func (s *Service) Doctor(fix bool) ([]models.DoctorIssue, int, error) {
	if !fix {
		return s.doctor(false)
	}

	var issues []models.DoctorIssue
	var fixed int
	err := s.State.WithLock(func() error {
		var err error
		issues, fixed, err = s.doctor(true)
		return err
	})
	return issues, fixed, err
}

func (s *Service) doctor(fix bool) ([]models.DoctorIssue, int, error) {
	workspaces, err := s.State.Load()
	if err != nil {
		return nil, 0, err
	}

	var issues []models.DoctorIssue
	fixed := 0

	for _, ws := range workspaces {
		f, iss := s.checkWorkspaceExists(ws, fix)
		if f > 0 {
			fixed += f
		}
		issues = append(issues, iss...)
		if len(iss) > 0 {
			continue
		}

		f, iss = s.checkWorkspaceRepos(&ws, fix)
		fixed += f
		issues = append(issues, iss...)
	}

	return issues, fixed, nil
}

func (s *Service) checkWorkspaceExists(ws models.Workspace, fix bool) (int, []models.DoctorIssue) {
	if _, err := os.Stat(ws.Path); err == nil {
		return 0, nil
	}
	issue := models.DoctorIssue{
		Workspace:       ws.Name,
		Repo:            nil,
		Issue:           "workspace directory missing",
		SuggestedAction: "remove stale state entry",
	}
	if fix {
		s.State.RemoveWorkspace(ws.Name)
		return 1, []models.DoctorIssue{issue}
	}
	return 0, []models.DoctorIssue{issue}
}

func (s *Service) checkWorkspaceRepos(ws *models.Workspace, fix bool) (int, []models.DoctorIssue) {
	var issues []models.DoctorIssue
	var toRemove []string
	fixed := 0

	for _, r := range ws.Repos {
		if iss, shouldRemove := s.checkRepo(ws.Name, r); iss != nil {
			issues = append(issues, *iss)
			if fix && shouldRemove {
				toRemove = append(toRemove, r.RepoName)
				fixed++
			}
		}
	}

	if fix && len(toRemove) > 0 {
		if currentWS, err := s.State.GetWorkspace(ws.Name); err == nil && currentWS != nil {
			for _, name := range toRemove {
				currentWS.RemoveRepo(name)
			}
			s.State.UpdateWorkspace(*currentWS)
		}
	}

	return fixed, issues
}

func (s *Service) checkRepo(wsName string, r models.RepoWorktree) (*models.DoctorIssue, bool) {
	repoName := r.RepoName

	if _, err := os.Stat(r.SourceRepo); os.IsNotExist(err) {
		return &models.DoctorIssue{
			Workspace:       wsName,
			Repo:            &repoName,
			Issue:           "source repo missing",
			SuggestedAction: "remove stale repo entry",
		}, true
	}

	if _, err := os.Stat(r.WorktreePath); os.IsNotExist(err) {
		return &models.DoctorIssue{
			Workspace:       wsName,
			Repo:            &repoName,
			Issue:           "worktree directory missing",
			SuggestedAction: "remove stale repo entry",
		}, true
	}

	return nil, false
}
