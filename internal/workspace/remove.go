package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
)

// RemoveOptions controls destructive worktree cleanup.
type RemoveOptions struct {
	Force             bool
	ExpectedCreatedAt string
	ExpectedPath      string
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
	if err := verifyExpectedWorkspace(ws, opts); err != nil {
		return err
	}

	if err := preflightRemovals(ws.Repos, opts.Force); err != nil {
		return err
	}

	// Teardown commands are user-owned and may invoke gw. Run them before the
	// mutation lock, then reload and preflight state again while locked.
	s.runTeardownHooks(ws.Repos)

	var deleted *models.Workspace
	var trashPath string
	if err := s.State.WithLock(func() error {
		var err error
		deleted, trashPath, err = s.deleteLocked(name, opts)
		return err
	}); err != nil {
		return err
	}

	// Bytes in .trash are no longer on the workspace path. Unlink is best-effort
	// and must not hold state.lock — leftover trash must not keep the workspace in state.
	if trashPath != "" {
		s.scheduleUnlink(trashPath)
	}

	s.Stats.RecordDeleted(*deleted)
	logging.Info("workspace %q deleted", name)
	console.Successf("Workspace %s deleted", name)
	return nil
}

func verifyExpectedWorkspace(ws *models.Workspace, opts RemoveOptions) error {
	if opts.ExpectedCreatedAt != "" && ws.CreatedAt != opts.ExpectedCreatedAt {
		return fmt.Errorf("workspace %s changed after pre-delete checks", ws.Name)
	}
	if opts.ExpectedPath != "" && ws.Path != opts.ExpectedPath {
		return fmt.Errorf("workspace %s changed after pre-delete checks", ws.Name)
	}
	return nil
}

func (s *Service) deleteLocked(name string, opts RemoveOptions) (*models.Workspace, string, error) {
	ws, err := s.State.GetWorkspace(name)
	if err != nil {
		return nil, "", err
	}
	if ws == nil {
		return nil, "", fmt.Errorf("workspace %s not found", name)
	}
	if err := verifyExpectedWorkspace(ws, opts); err != nil {
		return nil, "", err
	}
	if err := preflightRemovals(ws.Repos, opts.Force); err != nil {
		return nil, "", err
	}

	logging.Info("deleting workspace %q", name)
	original := *ws

	trashPath, err := quarantineWorkspace(ws.Path)
	if err != nil {
		return nil, "", fmt.Errorf("quarantining workspace %s: %w", ws.Path, err)
	}

	var pruneErrs []error
	for _, repo := range ws.Repos {
		if err := s.pruneWorktree(repo.SourceRepo); err != nil {
			pruneErrs = append(pruneErrs, fmt.Errorf("%s: pruning worktree: %w", repo.RepoName, err))
		}
	}
	if len(pruneErrs) > 0 {
		if restoreErr := s.restoreQuarantinedWorkspace(ws, trashPath); restoreErr != nil {
			return nil, "", errors.Join(append(pruneErrs, restoreErr)...)
		}
		return nil, "", errors.Join(pruneErrs...)
	}

	if err := s.removeState(name); err != nil {
		if restoreErr := s.restoreQuarantinedWorkspace(ws, trashPath); restoreErr != nil {
			return nil, "", errors.Join(err, restoreErr)
		}
		return nil, "", err
	}

	for _, repo := range original.Repos {
		s.deleteBranch(repo, opts.Force)
	}
	return &original, trashPath, nil
}

func (s *Service) restoreQuarantinedWorkspace(ws *models.Workspace, trashPath string) error {
	if renameErr := os.Rename(trashPath, ws.Path); renameErr != nil {
		return fmt.Errorf("restoring workspace root %s: %w", ws.Path, renameErr)
	}
	var repairErrs []error
	for _, repo := range ws.Repos {
		if err := s.repairWorktree(repo.SourceRepo, repo.WorktreePath); err != nil {
			repairErrs = append(repairErrs, fmt.Errorf("%s: repairing worktree: %w", repo.RepoName, err))
		}
	}
	return errors.Join(repairErrs...)
}

func (s *Service) repairWorktree(repo, path string) error {
	if s.RepairWorktree != nil {
		return s.RepairWorktree(repo, path)
	}
	return gitops.WorktreeRepair(repo, path)
}

func (s *Service) removeState(name string) error {
	if s.RemoveState != nil {
		return s.RemoveState(name)
	}
	return s.State.RemoveWorkspace(name)
}

const trashDirName = ".trash"

// UnlinkTrashPath removes a quarantined workspace directory. Paths whose parent
// is not .trash are rejected so a detached unlink process cannot delete arbitrary trees.
func UnlinkTrashPath(path string) error {
	cleaned := filepath.Clean(path)
	if !isTrashItem(cleaned) {
		return fmt.Errorf("refusing to unlink %s: not a grove trash item", path)
	}
	var err error
	for i := 0; i < 10; i++ {
		err = os.RemoveAll(cleaned)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

func isTrashItem(path string) bool {
	parent := filepath.Base(filepath.Dir(path))
	name := filepath.Base(path)
	return parent == trashDirName && name != "" && name != "." && name != ".." && name != trashDirName
}

func quarantineWorkspace(path string) (string, error) {
	trashRoot := filepath.Join(filepath.Dir(path), trashDirName)
	if err := os.MkdirAll(trashRoot, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(trashRoot, filepath.Base(path)+"-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (s *Service) scheduleUnlink(path string) {
	if s.StartUnlink != nil {
		if err := s.StartUnlink(path); err != nil {
			logging.Warn("failed to spawn unlink for %s: %s", path, err)
			s.syncUnlink(path)
		}
		return
	}
	s.syncUnlink(path)
}

func (s *Service) syncUnlink(path string) {
	if err := s.unlinkTrash(path); err != nil && !os.IsNotExist(err) {
		logging.Warn("failed to unlink quarantined workspace %s: %s", path, err)
	}
}

func (s *Service) unlinkTrash(path string) error {
	if s.UnlinkTrash != nil {
		return s.UnlinkTrash(path)
	}
	return UnlinkTrashPath(path)
}

func (s *Service) pruneWorktree(repo string) error {
	if s.PruneWorktree != nil {
		return s.PruneWorktree(repo)
	}
	return gitops.WorktreePrune(repo)
}

func (s *Service) runTeardownHooks(repos []models.RepoWorktree) {
	for _, repo := range repos {
		groveCfg, _ := gitops.ReadGroveConfig(repo.SourceRepo)
		if groveCfg != nil && groveCfg.Teardown != "" {
			s.RunCmdSilent(repo.WorktreePath, groveCfg.Teardown)
		}
	}
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
