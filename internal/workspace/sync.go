package workspace

import (
	"fmt"
	"sync"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
)

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
