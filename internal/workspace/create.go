package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

func (s *Service) finishCreate(ws models.Workspace) {
	s.Stats.RecordCreated(ws)
	logging.Info("workspace %q created at %s", ws.Name, ws.Path)
	console.Successf("Workspace %s created at %s", ws.Name, ws.Path)
	if cdFile := os.Getenv("GROVE_CD_FILE"); cdFile != "" {
		_ = os.WriteFile(cdFile, []byte(ws.Path), 0o644)
	}
}

func (s *Service) createWithOptsLocked(name string, opts CreateOpts) (models.Workspace, error) {
	branch := opts.Branch
	repoNames := opts.Repos
	repoMap := opts.RepoMap
	cfg := opts.Cfg

	existing, err := s.State.GetWorkspace(name)
	if err != nil {
		return models.Workspace{}, err
	}
	if existing != nil {
		return models.Workspace{}, fmt.Errorf("workspace %s already exists", name)
	}

	logging.Info("creating workspace %q (branch=%s, repos=%v)", name, branch, repoNames)

	if err := os.MkdirAll(cfg.WorkspaceDir, 0o755); err != nil {
		return models.Workspace{}, fmt.Errorf("creating workspace parent: %w", err)
	}
	wsPath := filepath.Join(cfg.WorkspaceDir, name)
	if err := os.Mkdir(wsPath, 0o755); err != nil {
		return models.Workspace{}, fmt.Errorf("creating workspace dir: %w", err)
	}

	ws := models.NewWorkspace(name, wsPath, branch)
	ws.Source = opts.Source

	// Validate all repo names before provisioning.
	sourcePaths := make([]string, len(repoNames))
	for i, repoName := range repoNames {
		sourcePath, ok := repoMap[repoName]
		if !ok {
			return models.Workspace{}, errors.Join(
				fmt.Errorf("repo %s not found", repoName),
				removeEmptyDir(wsPath),
			)
		}
		sourcePaths[i] = sourcePath
	}

	// Phase 1: parallel fetch (the slow network part).
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

	// Phase 2: sequential worktree creation (for rollback safety)
	var created []provisionedRepo
	for i, repoName := range repoNames {
		console.Infof("[%d/%d] %s", i+1, len(repoNames), repoName)
		mode := opts.BranchMode
		if opts.TrackBranchRepo != "" && repoName != opts.TrackBranchRepo {
			mode = BranchModeCreate
		}
		provisioned, err := provisionWorktreeNoFetch(sourcePaths[i], repoName, wsPath, branch, mode)
		if err != nil {
			logging.Error("workspace creation failed for %q — rolling back", name)
			return models.Workspace{}, errors.Join(
				fmt.Errorf("provisioning %s: %w", repoName, err),
				s.rollback(created),
				removeEmptyDir(wsPath),
			)
		}
		created = append(created, *provisioned)
		ws.Repos = append(ws.Repos, provisioned.worktree)
	}

	if err := s.State.AddWorkspace(ws); err != nil {
		return models.Workspace{}, errors.Join(err, s.rollback(created), removeEmptyDir(wsPath))
	}

	return ws, nil
}

type provisionedRepo struct {
	worktree      models.RepoWorktree
	branchCreated bool
}

func provisionWorktree(sourcePath, repoName, wsPath, branch string) (*provisionedRepo, error) {
	_ = gitops.Fetch(sourcePath)
	return provisionWorktreeNoFetch(sourcePath, repoName, wsPath, branch, BranchModeCreate)
}

func provisionWorktreeNoFetch(sourcePath, repoName, wsPath, branch string, mode BranchMode) (*provisionedRepo, error) {
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

	branchCreated, err := ensureWorkspaceBranch(sourcePath, repoName, branch)
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
			RepoName:     repoName,
			SourceRepo:   sourcePath,
			WorktreePath: wtPath,
			Branch:       branch,
		},
		branchCreated: branchCreated,
	}, nil
}

func ensureWorkspaceBranch(sourcePath, repoName, branch string) (bool, error) {
	if gitops.BranchExists(sourcePath, branch) {
		return false, nil
	}

	base, err := gitops.ResolveBaseBranch(sourcePath)
	if err != nil {
		base = "HEAD"
	}
	logging.Info("creating branch %q in %s from %s", branch, repoName, base)
	if err := gitops.CreateBranch(sourcePath, branch, base); err != nil {
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
