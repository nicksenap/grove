package workspace

import (
	"fmt"
	"sync"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
)

// Reset switches each worktree back to its recorded branch, then syncs.
// Dirty repos that are not on the recorded branch are skipped unless discard
// is set. Sync itself is unchanged: it still rebases whatever HEAD it is given.
func (s *Service) Reset(wsName string, discard bool) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %s not found", wsName)
	}

	logging.Info("resetting workspace %q", wsName)

	var wg sync.WaitGroup
	for _, r := range ws.Repos {
		wg.Add(1)
		go func(repo models.RepoWorktree) {
			defer wg.Done()
			s.resetOneRepo(repo, discard)
		}(r)
	}
	wg.Wait()

	return nil
}

func (s *Service) resetOneRepo(r models.RepoWorktree, discard bool) {
	live, err := gitops.CurrentBranch(r.WorktreePath)
	if err != nil {
		console.Warningf("%s: cannot determine current branch: %s", r.RepoName, err)
		return
	}
	if live == r.Branch {
		s.syncOneRepo(r)
		return
	}

	liveLabel := live
	if liveLabel == "" {
		liveLabel = "detached HEAD"
	}

	if !discard {
		status, err := gitops.RepoStatus(r.WorktreePath)
		if err != nil {
			console.Warningf("%s: status check failed: %s", r.RepoName, err)
			return
		}
		if status != "" {
			console.Warningf("%s: skipping (dirty working tree, on %s)", r.RepoName, liveLabel)
			return
		}
	}

	if err := gitops.Switch(r.WorktreePath, r.Branch, discard); err != nil {
		console.Warningf("%s: switch to %s failed: %s", r.RepoName, r.Branch, err)
		return
	}
	console.Infof("%s: switched %s → %s", r.RepoName, liveLabel, r.Branch)
	s.syncOneRepo(r)
}
