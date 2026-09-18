package workspace

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
)

// Reasons a branch is a prune candidate.
const (
	BranchReasonMerged = "merged"
	BranchReasonGone   = "gone"
)

// BranchCandidate is a local branch in a main clone that no worktree uses and
// that is merged into the repo's base branch or whose upstream was deleted.
type BranchCandidate struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Reason string `json:"reason"`
	Base   string `json:"base,omitempty"`
}

// BranchPruneOptions controls candidate selection.
type BranchPruneOptions struct {
	Fetch bool // run git fetch --prune before inspecting
	Gone  bool // also include branches whose upstream is gone
}

// BranchPruneRepo is a main clone to inspect.
type BranchPruneRepo struct {
	Name string
	Path string
}

// BranchPruneCandidates inspects repos concurrently and returns candidates
// sorted by repo then branch. Branches used by any worktree or referenced by a
// Grove workspace are never candidates, nor are the base branch or HEAD.
func BranchPruneCandidates(repos []BranchPruneRepo, workspaces []models.Workspace, opts BranchPruneOptions) ([]BranchCandidate, []error) {
	inUse := make(map[string]map[string]bool) // repo path → branch → true
	for _, ws := range workspaces {
		for _, r := range ws.Repos {
			if inUse[r.SourceRepo] == nil {
				inUse[r.SourceRepo] = make(map[string]bool)
			}
			inUse[r.SourceRepo][r.Branch] = true
		}
	}

	var (
		mu         sync.Mutex
		wg         sync.WaitGroup
		candidates []BranchCandidate
		errs       []error
	)
	for _, repo := range repos {
		wg.Add(1)
		go func(repo BranchPruneRepo) {
			defer wg.Done()
			found, err := branchCandidatesForRepo(repo, inUse[repo.Path], opts)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", repo.Name, err))
				return
			}
			candidates = append(candidates, found...)
		}(repo)
	}
	wg.Wait()
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Repo != candidates[j].Repo {
			return candidates[i].Repo < candidates[j].Repo
		}
		return candidates[i].Branch < candidates[j].Branch
	})
	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return candidates, errs
}

func branchCandidatesForRepo(repo BranchPruneRepo, inUse map[string]bool, opts BranchPruneOptions) ([]BranchCandidate, error) {
	if opts.Fetch {
		if err := gitops.FetchPrune(repo.Path); err != nil {
			return nil, fmt.Errorf("fetch: %w", err)
		}
	}
	base, err := gitops.ResolveBaseBranch(repo.Path)
	if err != nil {
		// No origin/default branch: nothing to be merged into. Skip quietly.
		return nil, nil
	}
	baseName := strings.TrimPrefix(base, "origin/")

	branches, err := gitops.LocalBranches(repo.Path)
	if err != nil {
		return nil, err
	}
	merged, err := gitops.MergedBranches(repo.Path, base)
	if err != nil {
		return nil, err
	}
	worktrees, err := gitops.WorktreeList(repo.Path)
	if err != nil {
		return nil, err
	}
	checkedOut := make(map[string]bool, len(worktrees))
	for _, wt := range worktrees {
		if wt.Branch != "" {
			checkedOut[wt.Branch] = true
		}
	}

	var out []BranchCandidate
	for _, b := range branches {
		if b.Head || b.Name == baseName || checkedOut[b.Name] || inUse[b.Name] {
			continue
		}
		switch {
		case merged[b.Name]:
			out = append(out, BranchCandidate{Repo: repo.Name, Path: repo.Path, Branch: b.Name, Reason: BranchReasonMerged, Base: base})
		case opts.Gone && b.Gone:
			out = append(out, BranchCandidate{Repo: repo.Name, Path: repo.Path, Branch: b.Name, Reason: BranchReasonGone})
		}
	}
	return out, nil
}

// DeleteBranchCandidate removes the branch. Merged branches use a safe delete;
// gone branches may hold unmerged commits and require force.
func DeleteBranchCandidate(c BranchCandidate) error {
	return gitops.DeleteBranch(c.Path, c.Branch, c.Reason == BranchReasonGone)
}
