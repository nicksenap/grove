package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// The machine CLI contract promises per-repo results for every mutating
// operation, so these tests pin the outcomes rather than just "no error".

func outcomeOf(t *testing.T, results []RepoResult, repo string) RepoResult {
	t.Helper()
	for _, r := range results {
		if r.Repo == repo {
			return r
		}
	}
	t.Fatalf("no result for repo %q in %+v", repo, results)
	return RepoResult{}
}

func TestCreateResultReportsEveryRepo(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	result, err := env.svc.CreateWithOpts("res-ws", CreateOpts{
		Branch:  "feat/res",
		Repos:   []string{"api", "web"},
		RepoMap: env.repoMap,
		Cfg:     env.cfg,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if result.Name != "res-ws" || result.Branch != "feat/res" {
		t.Errorf("result identity = %s/%s", result.Name, result.Branch)
	}
	if len(result.Repos) != 2 {
		t.Fatalf("expected 2 repo results, got %d", len(result.Repos))
	}
	for _, repo := range []string{"api", "web"} {
		r := outcomeOf(t, result.Repos, repo)
		if r.Outcome != OutcomeCreated {
			t.Errorf("%s outcome = %s, want %s", repo, r.Outcome, OutcomeCreated)
		}
		if r.Path == "" || r.Branch != "feat/res" {
			t.Errorf("%s result missing path/branch: %+v", repo, r)
		}
		if _, err := os.Stat(r.Path); err != nil {
			t.Errorf("%s reported path %s does not exist: %v", repo, r.Path, err)
		}
	}
}

// Create is all-or-nothing, so a failure must not report a half-built workspace.
func TestCreateFailureReturnsNoResult(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	result, err := env.svc.CreateWithOpts("bad-ws", CreateOpts{
		Branch:  "feat/bad",
		Repos:   []string{"api", "ghost"},
		RepoMap: env.repoMap,
		Cfg:     env.cfg,
	})
	if err == nil {
		t.Fatal("expected an error for an unknown repo")
	}
	if result != nil {
		t.Errorf("expected no result on failure, got %+v", result)
	}
}

func TestDeleteResultReportsRemovals(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("del-res", "feat/del", []string{"api", "web"}, env.repoMap, env.cfg)

	result, err := env.svc.Delete("del-res")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !result.StateRemoved {
		t.Error("a clean delete should remove the state entry")
	}
	if len(result.Repos) != 2 {
		t.Fatalf("expected 2 repo results, got %d", len(result.Repos))
	}
	for _, r := range result.Repos {
		if r.Outcome != OutcomeRemoved {
			t.Errorf("%s outcome = %s (%s), want %s", r.Repo, r.Outcome, r.Detail, OutcomeRemoved)
		}
	}
}

func TestSyncResultDistinguishesOutcomes(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("clean")
	env.createRepoWithRemote("dirty")
	env.svc.Create("sync-res", "feat/sync", []string{"clean", "dirty"}, env.repoMap, env.cfg)

	// Leave one worktree dirty; sync must skip it rather than fail the command.
	dirtyPath := filepath.Join(env.wsDir, "sync-res", "dirty")
	os.WriteFile(filepath.Join(dirtyPath, "scratch.txt"), []byte("wip"), 0o644)

	result, err := env.svc.Sync("sync-res")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	clean := outcomeOf(t, result.Repos, "clean")
	if clean.Outcome != OutcomeUpToDate {
		t.Errorf("clean repo outcome = %s (%s), want %s", clean.Outcome, clean.Detail, OutcomeUpToDate)
	}

	dirty := outcomeOf(t, result.Repos, "dirty")
	if dirty.Outcome != OutcomeSkipped {
		t.Errorf("dirty repo outcome = %s, want %s", dirty.Outcome, OutcomeSkipped)
	}
	if dirty.Detail == "" {
		t.Error("a skipped repo must explain why")
	}
}

func TestSyncReportsRebase(t *testing.T) {
	env := setupTestEnv(t)
	clone := env.createRepoWithRemote("api")
	env.svc.Create("rebase-res", "feat/rebase", []string{"api"}, env.repoMap, env.cfg)

	// Advance the base branch so the workspace branch is behind.
	base := env.run(clone, "git", "branch", "--show-current")
	os.WriteFile(filepath.Join(clone, "upstream.txt"), []byte("new"), 0o644)
	env.run(clone, "git", "add", ".")
	env.run(clone, "git", "commit", "-q", "-m", "upstream work")
	env.run(clone, "git", "push", "-q", "origin", base)

	result, err := env.svc.Sync("rebase-res")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	api := outcomeOf(t, result.Repos, "api")
	if api.Outcome != OutcomeRebased {
		t.Fatalf("outcome = %s (%s), want %s", api.Outcome, api.Detail, OutcomeRebased)
	}
	if api.Detail == "" {
		t.Error("a rebase should report what it moved onto")
	}
}

// Adding a repo that is already there is a no-op, not a failure: an agent
// retrying after a partial failure must be able to converge.
func TestAddReposReportsAlreadyPresent(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("add-res", "feat/add", []string{"api"}, env.repoMap, env.cfg)

	result, err := env.svc.AddRepos("add-res", []string{"api", "web"}, env.repoMap)
	if err != nil {
		t.Fatalf("add-repo: %v", err)
	}
	if got := outcomeOf(t, result.Repos, "api"); got.Outcome != OutcomeAlreadyExists {
		t.Errorf("api outcome = %s, want %s", got.Outcome, OutcomeAlreadyExists)
	}
	if got := outcomeOf(t, result.Repos, "web"); got.Outcome != OutcomeAdded {
		t.Errorf("web outcome = %s, want %s", got.Outcome, OutcomeAdded)
	}
}

// Likewise, removing a repo that is not in the workspace converges instead of
// failing.
func TestRemoveReposReportsNotFound(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("rm-res", "feat/rm", []string{"api", "web"}, env.repoMap, env.cfg)

	result, err := env.svc.RemoveRepos("rm-res", []string{"web", "ghost"})
	if err != nil {
		t.Fatalf("remove-repo: %v", err)
	}
	if got := outcomeOf(t, result.Repos, "web"); got.Outcome != OutcomeRemoved {
		t.Errorf("web outcome = %s (%s), want %s", got.Outcome, got.Detail, OutcomeRemoved)
	}
	if got := outcomeOf(t, result.Repos, "ghost"); got.Outcome != OutcomeNotFound {
		t.Errorf("ghost outcome = %s, want %s", got.Outcome, OutcomeNotFound)
	}

	ws, _ := env.svc.State.GetWorkspace("rm-res")
	if ws.FindRepo("web") != nil {
		t.Error("web should be gone from state")
	}
}

// Removing a single repo must not force-delete its branch — unmerged work is
// preserved, unlike deleting the whole workspace.
func TestRemoveReposKeepsUnmergedBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("keep-branch", "feat/keep", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "keep-branch", "api")
	os.WriteFile(filepath.Join(wt, "work.txt"), []byte("unmerged"), 0o644)
	env.run(wt, "git", "add", ".")
	env.run(wt, "git", "commit", "-q", "-m", "unmerged work")

	if _, err := env.svc.RemoveRepos("keep-branch", []string{"api"}); err != nil {
		t.Fatalf("remove-repo: %v", err)
	}

	branches := env.run(env.repoMap["api"], "git", "branch", "--list", "feat/keep")
	if branches == "" {
		t.Error("an unmerged branch should survive remove-repo")
	}
}

func TestFailedReposFiltersResults(t *testing.T) {
	results := []RepoResult{
		{Repo: "a", Outcome: OutcomeRemoved},
		{Repo: "b", Outcome: OutcomeFailed},
		{Repo: "c", Outcome: OutcomeFailed},
	}
	failed := FailedRepos(results)
	if len(failed) != 2 || failed[0] != "b" || failed[1] != "c" {
		t.Errorf("FailedRepos = %v, want [b c]", failed)
	}
	if FailedRepos(results[:1]) != nil {
		t.Error("FailedRepos should be empty when nothing failed")
	}
}
