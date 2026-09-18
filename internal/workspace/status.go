package workspace

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/output"
)

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

// StatusOptions controls status output.
type StatusOptions struct {
	Format  output.Format
	Verbose bool
	PR      bool
	Stdout  io.Writer
	Stderr  io.Writer
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

	format := opts.Format
	if format == "" {
		format = output.Table
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if err := writeStatus(stdout, ws, results, format, opts); err != nil {
		return err
	}
	printVerboseStatus(stderr, results, opts)
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
