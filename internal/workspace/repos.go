package workspace

import (
	"errors"
	"fmt"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
)

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
