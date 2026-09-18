package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
)

func TestBranchPruneCandidatesSelectsOnlyUnusedMergedOrGoneBranches(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")
	base := env.run(repo, "git", "branch", "--show-current")

	// merged: branched from base, no new commits.
	env.run(repo, "git", "branch", "feat/merged")
	// unmerged: has a commit not on base.
	env.run(repo, "git", "checkout", "-q", "-b", "feat/unmerged")
	os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x"), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "wip")
	// gone: pushed with upstream, then deleted on origin.
	env.run(repo, "git", "checkout", "-q", "-b", "feat/gone")
	os.WriteFile(filepath.Join(repo, "y.txt"), []byte("y"), 0o644)
	env.run(repo, "git", "add", ".")
	env.run(repo, "git", "commit", "-q", "-m", "gone")
	env.run(repo, "git", "push", "-q", "-u", "origin", "feat/gone")
	env.run(repo, "git", "push", "-q", "origin", "--delete", "feat/gone")
	env.run(repo, "git", "checkout", "-q", base)
	// in a worktree: merged but must be skipped.
	env.run(repo, "git", "branch", "feat/in-worktree")
	if err := env.createWorkspace("ws", "feat/in-worktree", []string{"api"}); err != nil {
		t.Fatal(err)
	}
	workspaces, _ := env.svc.State.Load()

	repos := []BranchPruneRepo{{Name: "api", Path: repo}}
	got, errs := BranchPruneCandidates(repos, workspaces, BranchPruneOptions{Fetch: true})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(got) != 1 || got[0].Branch != "feat/merged" || got[0].Reason != BranchReasonMerged {
		t.Fatalf("default candidates = %+v", got)
	}

	got, _ = BranchPruneCandidates(repos, workspaces, BranchPruneOptions{Fetch: true, Gone: true})
	if len(got) != 2 || got[0].Branch != "feat/gone" || got[0].Reason != BranchReasonGone || got[1].Branch != "feat/merged" {
		t.Fatalf("gone candidates = %+v", got)
	}

	for _, c := range got {
		if err := DeleteBranchCandidate(c); err != nil {
			t.Fatalf("delete %s: %v", c.Branch, err)
		}
	}
	for _, b := range []string{"feat/merged", "feat/gone"} {
		if gitops.BranchExists(repo, b) {
			t.Errorf("%s still exists", b)
		}
	}
	for _, b := range []string{base, "feat/unmerged", "feat/in-worktree"} {
		if !gitops.BranchExists(repo, b) {
			t.Errorf("%s was deleted", b)
		}
	}
}

func TestBranchPruneCandidatesSkipsBranchesReferencedByState(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepoWithRemote("api")
	env.run(repo, "git", "branch", "feat/stale-state")
	workspaces := []models.Workspace{{Name: "ghost", Repos: []models.RepoWorktree{{SourceRepo: repo, Branch: "feat/stale-state"}}}}

	got, errs := BranchPruneCandidates([]BranchPruneRepo{{Name: "api", Path: repo}}, workspaces, BranchPruneOptions{})
	if len(errs) != 0 || len(got) != 0 {
		t.Fatalf("candidates = %+v errs = %v", got, errs)
	}
}
