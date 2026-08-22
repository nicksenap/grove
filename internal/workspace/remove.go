package workspace

import (
	"errors"
	"fmt"
	"os"

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

func verifyExpectedWorkspace(ws *models.Workspace, opts RemoveOptions) error {
	if opts.ExpectedCreatedAt != "" && ws.CreatedAt != opts.ExpectedCreatedAt {
		return fmt.Errorf("workspace %s changed after pre-delete checks", ws.Name)
	}
	if opts.ExpectedPath != "" && ws.Path != opts.ExpectedPath {
		return fmt.Errorf("workspace %s changed after pre-delete checks", ws.Name)
	}
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
	if err := verifyExpectedWorkspace(ws, opts); err != nil {
		return nil, err
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

	// Once all registered worktrees are safely removed, everything remaining in
	// this Grove-owned root is workspace metadata and must not block deletion.
	if removeRootErr := os.RemoveAll(ws.Path); removeRootErr != nil && !os.IsNotExist(removeRootErr) {
		ws.Repos = nil
		cleanupErrs = append(cleanupErrs, fmt.Errorf("removing workspace root %s: %w", ws.Path, removeRootErr))
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

	console.Infof("Adding %d %s to %s. Please wait.", len(toAdd), repoNoun(len(toAdd)), wsName)

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
	if len(selected) == 0 {
		return nil
	}

	console.Infof("Removing %d %s from %s. Please wait.", len(selected), repoNoun(len(selected)), wsName)
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

func repoNoun(count int) string {
	if count == 1 {
		return "repo"
	}
	return "repos"
}
