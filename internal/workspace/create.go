package workspace

import (
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

// CreateWithOpts creates a new workspace from the given options, including
// per-repo branch tracking (BranchMode/TrackBranchRepo) and a persisted Source link.
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
