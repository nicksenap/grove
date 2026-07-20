package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
)

// installCreateFaultBackend wraps the env's production backend so create tests
// can inject failures at specific phases.
func installCreateFaultBackend(env *testEnv) *faultBackend {
	fb := newFaultBackend(prodBackend{})
	env.svc.backend = fb
	return fb
}

func recordFor(t *testing.T, env *testEnv, kind state.OperationKind, ws string) *state.OperationRecord {
	t.Helper()
	recs, err := env.svc.ops().List()
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	for i := range recs {
		if recs[i].Kind == kind && recs[i].Workspace == ws {
			return &recs[i]
		}
	}
	return nil
}

func branchExists(env *testEnv, repo, branch string) bool {
	out := env.run(env.repoMap[repo], "git", "branch", "--list", branch)
	return out != ""
}

// TestCreateWorktreeFailureCompensatesBranch proves a worktree-add failure after
// a successful branch create rolls back the operation-created branch and leaves
// no state, worktree, or record behind.
func TestCreateWorktreeFailureCompensatesBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	fb := installCreateFaultBackend(env)
	// Branch is created, then the worktree add fails before applying.
	fb.failBefore["WorktreeAdd:"+env.repoMap["api"]] = errors.New("disk full")

	res := env.svc.CreateWithResult("wt-fail", CreateOpts{
		Branch: "feat/x", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeFailed {
		t.Fatalf("expected failed outcome, got %s (%v)", res.Status, res.Err)
	}
	if !res.NonZeroExit() {
		t.Fatal("failed create must exit non-zero")
	}
	// State: nothing committed.
	if ws, _ := env.svc.State.GetWorkspace("wt-fail"); ws != nil {
		t.Fatal("no workspace should be committed")
	}
	// Branch: operation-created branch removed.
	if branchExists(env, "api", "feat/x") {
		t.Fatal("operation-created branch should be deleted on compensation")
	}
	// Record cleared after full compensation.
	if rec := recordFor(t, env, state.OpCreate, "wt-fail"); rec != nil {
		t.Fatalf("recovery record should be cleared, got %+v", rec)
	}
	// Workspace root removed.
	if _, err := os.Stat(filepath.Join(env.wsDir, "wt-fail")); !os.IsNotExist(err) {
		t.Fatal("workspace root should be removed")
	}
}

// TestCreatePreservesPreexistingBranch proves compensation never deletes a
// branch that existed before the operation.
func TestCreatePreservesPreexistingBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	// Pre-create the branch so the operation does NOT own it.
	env.run(env.repoMap["api"], "git", "branch", "feat/keep")

	fb := installCreateFaultBackend(env)
	fb.failAfter["WorktreeAdd:"+env.repoMap["api"]] = errors.New("boom")

	res := env.svc.CreateWithResult("keepbr", CreateOpts{
		Branch: "feat/keep", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeFailed {
		t.Fatalf("expected failed, got %s", res.Status)
	}
	if !branchExists(env, "api", "feat/keep") {
		t.Fatal("pre-existing branch must be preserved on compensation")
	}
}

// TestCreateLaterRepoFailureRollsBackEarlier proves a failure on the second repo
// compensates the first repo's created resources.
func TestCreateLaterRepoFailureRollsBackEarlier(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	fb := installCreateFaultBackend(env)
	fb.failBefore["WorktreeAdd:"+env.repoMap["web"]] = errors.New("web fails")

	res := env.svc.CreateWithResult("multi", CreateOpts{
		Branch: "feat/m", Repos: []string{"api", "web"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeFailed {
		t.Fatalf("expected failed, got %s (%v)", res.Status, res.Err)
	}
	// Ordered outcomes: api done, web failed.
	if len(res.Repos) != 2 || res.Repos[0].RepoName != "api" || res.Repos[1].RepoName != "web" {
		t.Fatalf("outcomes should preserve request order: %+v", res.Repos)
	}
	if res.Repos[1].Status != state.RepoFailed {
		t.Fatalf("web should be failed: %+v", res.Repos[1])
	}
	if branchExists(env, "api", "feat/m") {
		t.Fatal("earlier repo's created branch must be rolled back")
	}
	if _, err := os.Stat(filepath.Join(env.wsDir, "multi", "api")); !os.IsNotExist(err) {
		t.Fatal("earlier repo worktree must be removed")
	}
}

// TestCreateCommitFailureCompensates proves a final state-commit failure fully
// compensates provisioned resources.
func TestCreateCommitFailureCompensates(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	// Fail the commit itself (pre-commit: state not applied).
	env.svc.commitFault = func(m *state.Mutation) error { return errors.New("commit boom") }

	res := env.svc.CreateWithResult("commitfail", CreateOpts{
		Branch: "feat/c", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeFailed {
		t.Fatalf("expected failed, got %s (%v)", res.Status, res.Err)
	}
	if ws, _ := env.svc.State.GetWorkspace("commitfail"); ws != nil {
		t.Fatal("no workspace should be committed")
	}
	if branchExists(env, "api", "feat/c") {
		t.Fatal("branch should be rolled back after commit failure")
	}
	if rec := recordFor(t, env, state.OpCreate, "commitfail"); rec != nil {
		t.Fatal("record should be cleared after full compensation")
	}
}

// TestCreateRollbackFailureLeavesPendingRecord proves that when compensation
// itself fails, the operation is Pending and the recovery record is retained.
func TestCreateRollbackFailureLeavesPendingRecord(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	fb := installCreateFaultBackend(env)
	// Branch created, worktree add fails, then the branch delete (compensation)
	// also fails — leaving the operation unable to fully roll back.
	fb.failBefore["WorktreeAdd:"+env.repoMap["api"]] = errors.New("provision boom")
	fb.failBefore["DeleteBranch:"+env.repoMap["api"]] = errors.New("rollback boom")

	res := env.svc.CreateWithResult("pending-ws", CreateOpts{
		Branch: "feat/p", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomePending {
		t.Fatalf("expected pending, got %s (%v)", res.Status, res.Err)
	}
	if res.RecordID == "" {
		t.Fatal("pending result must carry a recovery record id")
	}
	rec := recordFor(t, env, state.OpCreate, "pending-ws")
	if rec == nil {
		t.Fatal("recovery record must be retained on incomplete rollback")
	}
	if rec.Phase != "compensation" || !rec.Retryable {
		t.Fatalf("record should be marked compensation/retryable: %+v", rec)
	}
}

// TestCreateSetupHookFailureIsPartial proves setup-hook failure after commit
// yields a partial (non-zero) outcome without undoing the valid workspace.
func TestCreateSetupHookFailureIsPartial(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("hooked")
	// A setup hook that fails.
	if err := os.WriteFile(filepath.Join(repo, ".grove.toml"), []byte(`setup = "exit 1"`), 0o644); err != nil {
		t.Fatal(err)
	}

	res := env.svc.CreateWithResult("hook-partial", CreateOpts{
		Branch: "feat/h", Repos: []string{"hooked"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomePartial {
		t.Fatalf("expected partial, got %s (%v)", res.Status, res.Err)
	}
	if !res.NonZeroExit() {
		t.Fatal("partial create must exit non-zero")
	}
	// Workspace is valid and committed.
	if ws, _ := env.svc.State.GetWorkspace("hook-partial"); ws == nil {
		t.Fatal("workspace must remain committed despite hook failure")
	}
	// Record cleared (commit succeeded).
	if rec := recordFor(t, env, state.OpCreate, "hook-partial"); rec != nil {
		t.Fatal("record should be cleared after successful commit")
	}
}

// TestCreateSuccessClearsRecord proves the happy path leaves no recovery record
// and a consistent workspace.
func TestCreateSuccessClearsRecord(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	res := env.svc.CreateWithResult("clean", CreateOpts{
		Branch: "feat/ok", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeSuccess {
		t.Fatalf("expected success, got %s (%v)", res.Status, res.Err)
	}
	if rec := recordFor(t, env, state.OpCreate, "clean"); rec != nil {
		t.Fatal("record should be cleared on success")
	}
	if ws, _ := env.svc.State.GetWorkspace("clean"); ws == nil || len(ws.Repos) != 1 {
		t.Fatal("workspace should be committed with its repo")
	}
}

// TestCreateResumesStalePendingRecord proves gw create can finish/roll back its
// own prior interrupted create for the same name.
func TestCreateResumesStalePendingRecord(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	// Simulate a prior crash: an operation-created branch + a stale record, but
	// no committed state.
	env.run(env.repoMap["api"], "git", "branch", "feat/stale")
	stale := &state.OperationRecord{
		Kind:          state.OpCreate,
		Workspace:     "resumed",
		Path:          filepath.Join(env.wsDir, "resumed"),
		RootOwnership: state.OwnUnknown,
		Repos: []state.RepoOperation{{
			RepoName:        "api",
			SourceRepo:      env.repoMap["api"],
			Branch:          "feat/stale",
			Status:          state.RepoDone,
			BranchOwnership: state.OwnCreated,
		}},
	}
	if err := env.svc.ops().Write(stale); err != nil {
		t.Fatal(err)
	}

	// A fresh create for the same name resolves the stale record first.
	res := env.svc.CreateWithResult("resumed", CreateOpts{
		Branch: "feat/new", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeSuccess {
		t.Fatalf("expected success after resume, got %s (%v)", res.Status, res.Err)
	}
	// Stale operation-created branch was rolled back during resume.
	if branchExists(env, "api", "feat/stale") {
		t.Fatal("stale operation-created branch should be rolled back on resume")
	}
	// Only one record-free, consistent workspace remains.
	recs, _ := env.svc.ops().List()
	if len(recs) != 0 {
		t.Fatalf("no records should remain, got %d", len(recs))
	}
}

// TestCreateInvalidInputsNoMutation proves precondition failures mutate nothing
// and write no recovery record.
func TestCreateInvalidInputsNoMutation(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	cases := []struct {
		name string
		opts CreateOpts
	}{
		{"", CreateOpts{Branch: "b", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg}},
		{"bad/name", CreateOpts{Branch: "b", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg}},
		{"no-branch", CreateOpts{Branch: "", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg}},
		{"unknown-repo", CreateOpts{Branch: "b", Repos: []string{"ghost"}, RepoMap: env.repoMap, Cfg: env.cfg}},
	}
	for _, c := range cases {
		res := env.svc.CreateWithResult(c.name, c.opts)
		if res.Status != OutcomeFailed || res.Err == nil {
			t.Fatalf("case %q: expected precondition failure, got %s", c.name, res.Status)
		}
	}
	recs, _ := env.svc.ops().List()
	if len(recs) != 0 {
		t.Fatalf("precondition failures must write no records, got %d", len(recs))
	}
}

// TestCreateSameNameConcurrency proves two concurrent creates for the same name
// yield exactly one success and one deterministic conflict, with the loser fully
// compensated.
func TestCreateSameNameConcurrency(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	type out struct {
		res *OperationResult
	}
	ch := make(chan out, 2)
	go func() {
		e := &Service{State: env.svc.State, Stats: env.svc.Stats, Ops: env.svc.Ops, RunCmd: prodRunCmd, RunCmdSilent: prodRunCmdSilent, backend: prodBackend{}}
		ch <- out{e.CreateWithResult("race", CreateOpts{Branch: "feat/a", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg})}
	}()
	go func() {
		e := &Service{State: env.svc.State, Stats: env.svc.Stats, Ops: env.svc.Ops, RunCmd: prodRunCmd, RunCmdSilent: prodRunCmdSilent, backend: prodBackend{}}
		ch <- out{e.CreateWithResult("race", CreateOpts{Branch: "feat/b", Repos: []string{"web"}, RepoMap: env.repoMap, Cfg: env.cfg})}
	}()
	a := <-ch
	b := <-ch

	successes := 0
	if a.res.Status == OutcomeSuccess {
		successes++
	}
	if b.res.Status == OutcomeSuccess {
		successes++
	}
	if successes != 1 {
		t.Fatalf("expected exactly one winner, got %d (a=%s b=%s)", successes, a.res.Status, b.res.Status)
	}
	// Exactly one workspace named "race" in state, and its recorded repos match
	// its on-disk worktrees (winner not corrupted by the loser's rollback).
	ws, _ := env.svc.State.GetWorkspace("race")
	if ws == nil {
		t.Fatal("winner should be committed")
	}
	if _, err := os.Stat(ws.Path); err != nil {
		t.Fatalf("winner workspace root must exist: %v", err)
	}
	for _, r := range ws.Repos {
		if _, err := os.Stat(r.WorktreePath); err != nil {
			t.Fatalf("winner worktree %s must exist: %v", r.WorktreePath, err)
		}
		if !branchExists(env, r.RepoName, r.Branch) {
			t.Fatalf("winner branch %s must exist in %s", r.Branch, r.RepoName)
		}
	}
	// No recovery records remain for either attempt.
	recs, _ := env.svc.ops().List()
	if len(recs) != 0 {
		t.Fatalf("no records should remain after race, got %d", len(recs))
	}
	_ = context.Background()
}

// TestCreateAmbiguousCommitReconciledAsCommitted proves that when the state
// commit applies but returns an error (e.g. a post-rename dir-sync failure), the
// workspace is reconciled as committed rather than destroyed.
func TestCreateAmbiguousCommitReconciledAsCommitted(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	// commitFault commits, then returns an error (ambiguous commit).
	env.svc.commitFault = func(m *state.Mutation) error {
		if e := m.Commit(); e != nil {
			return e
		}
		return errors.New("dir sync after rename")
	}

	res := env.svc.CreateWithResult("ambi", CreateOpts{
		Branch: "feat/a", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	// The workspace persisted, so it must NOT be compensated; treat as committed.
	if ws, _ := env.svc.State.GetWorkspace("ambi"); ws == nil {
		t.Fatal("ambiguous-but-applied commit must keep the workspace")
	}
	if !branchExists(env, "api", "feat/a") {
		t.Fatal("branch must be preserved for an applied commit")
	}
	if res.Status != OutcomeSuccess && res.Status != OutcomePartial {
		t.Fatalf("expected success/partial for applied commit, got %s", res.Status)
	}
}

// TestCompensateWorktreeReconcilesStaleRegistration proves compensation
// reconciles against the Git worktree registration, not just the filesystem:
// when a worktree directory is deleted but its registration lingers (prunable),
// compensation prunes the registration and leaves the branch untouched.
func TestCompensateWorktreeReconcilesStaleRegistration(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	wtPath := filepath.Join(t.TempDir(), "wt")
	env.run(repo, "git", "branch", "feat/keep")
	env.run(repo, "git", "worktree", "add", wtPath, "feat/keep")

	// Delete the directory out-of-band, leaving a prunable registration.
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatal(err)
	}
	if reg, _ := env.svc.worktreeRegistered(repo, wtPath); !reg {
		t.Fatal("precondition: stale registration should still exist")
	}

	rw := models.RepoWorktree{RepoName: "api", SourceRepo: repo, WorktreePath: wtPath, Branch: "feat/keep"}
	if err := env.svc.compensateWorktree(rw); err != nil {
		t.Fatalf("compensateWorktree: %v", err)
	}
	if reg, _ := env.svc.worktreeRegistered(repo, wtPath); reg {
		t.Fatal("stale worktree registration must be pruned")
	}
	// compensateWorktree must not touch branches.
	if !branchExists(env, "api", "feat/keep") {
		t.Fatal("branch must remain after worktree compensation")
	}
}

// TestCompensateWorktreePreservesUnrelatedRegistrations proves worktree
// compensation is path-scoped: removing one stale registration must not disturb
// another workspace's prunable registration in the same repo.
func TestCompensateWorktreePreservesUnrelatedRegistrations(t *testing.T) {
	env := setupTestEnv(t)
	repo := env.createRepo("api")
	mine := filepath.Join(t.TempDir(), "mine")
	other := filepath.Join(t.TempDir(), "other")
	env.run(repo, "git", "branch", "b-mine")
	env.run(repo, "git", "branch", "b-other")
	env.run(repo, "git", "worktree", "add", mine, "b-mine")
	env.run(repo, "git", "worktree", "add", other, "b-other")
	// Both directories vanish, leaving two prunable registrations.
	os.RemoveAll(mine)
	os.RemoveAll(other)

	rw := models.RepoWorktree{RepoName: "api", SourceRepo: repo, WorktreePath: mine, Branch: "b-mine"}
	if err := env.svc.compensateWorktree(rw); err != nil {
		t.Fatalf("compensateWorktree: %v", err)
	}
	if reg, _ := env.svc.worktreeRegistered(repo, mine); reg {
		t.Fatal("my stale registration must be removed")
	}
	if reg, _ := env.svc.worktreeRegistered(repo, other); !reg {
		t.Fatal("unrelated prunable registration must be preserved (scoped removal only)")
	}
}

// TestCreateMkdirFailureNoRecord proves a workspace-root mkdir failure leaves no
// state and no recovery record.
func TestCreateMkdirFailureNoRecord(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	fb := installCreateFaultBackend(env)
	wsPath := filepath.Join(env.wsDir, "mkfail")
	fb.failBefore["Mkdir:"+wsPath] = errors.New("permission denied")

	res := env.svc.CreateWithResult("mkfail", CreateOpts{
		Branch: "feat/m", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeFailed {
		t.Fatalf("expected failed, got %s", res.Status)
	}
	if recs, _ := env.svc.ops().List(); len(recs) != 0 {
		t.Fatalf("mkdir failure should leave no record, got %d", len(recs))
	}
}

// TestCreateMkdirAppliedThenErrorCompensatesRoot proves an applied-then-error
// mkdir (root created, then error) removes the operation-created root and
// clears the record.
func TestCreateMkdirAppliedThenErrorCompensatesRoot(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	fb := installCreateFaultBackend(env)
	wsPath := filepath.Join(env.wsDir, "mkafter")
	fb.failAfter["Mkdir:"+wsPath] = errors.New("post-mkdir boom")

	res := env.svc.CreateWithResult("mkafter", CreateOpts{
		Branch: "feat/m", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeFailed {
		t.Fatalf("expected failed, got %s (%v)", res.Status, res.Err)
	}
	if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
		t.Fatal("operation-created root must be removed after applied-then-error mkdir")
	}
	if recs, _ := env.svc.ops().List(); len(recs) != 0 {
		t.Fatalf("record should be cleared, got %d", len(recs))
	}
}

// TestCreateDifferentNameSameBranchConcurrency proves two creates for DIFFERENT
// workspace names targeting the same branch in the same repo do not delete each
// other's branch: the branch resource lock serializes them and the loser sees
// the branch as pre-existing (never compensating it).
func TestCreateDifferentNameSameBranchConcurrency(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	type out struct{ res *OperationResult }
	ch := make(chan out, 2)
	mk := func(ws string) {
		e := &Service{State: env.svc.State, Stats: env.svc.Stats, Ops: env.svc.Ops, RunCmd: prodRunCmd, RunCmdSilent: prodRunCmdSilent, backend: prodBackend{}}
		ch <- out{e.CreateWithResult(ws, CreateOpts{Branch: "feat/shared", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg})}
	}
	go mk("ws-a")
	go mk("ws-b")
	a := <-ch
	b := <-ch

	successes := 0
	if a.res.Status == OutcomeSuccess {
		successes++
	}
	if b.res.Status == OutcomeSuccess {
		successes++
	}
	if successes != 1 {
		t.Fatalf("exactly one should succeed (one worktree per branch), got %d", successes)
	}
	// The winner's branch must survive the loser's compensation.
	if !branchExists(env, "api", "feat/shared") {
		t.Fatal("shared branch must survive the loser's rollback")
	}
}

// TestCreatePendingRecordRetryConverges proves that after a rollback-incomplete
// pending record, re-running create for the same name resumes: it compensates
// the retained resources and then succeeds, leaving no record.
func TestCreatePendingRecordRetryConverges(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	fb := installCreateFaultBackend(env)
	fb.failBefore["WorktreeAdd:"+env.repoMap["api"]] = errors.New("provision boom")
	fb.failBefore["DeleteBranch:"+env.repoMap["api"]] = errors.New("rollback boom")

	if res := env.svc.CreateWithResult("conv", CreateOpts{
		Branch: "feat/c", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	}); res.Status != OutcomePending {
		t.Fatalf("setup: expected pending, got %s", res.Status)
	}

	// Clear the faults and retry: resume should compensate then create cleanly.
	env.svc.backend = prodBackend{}
	res := env.svc.CreateWithResult("conv", CreateOpts{
		Branch: "feat/c", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeSuccess {
		t.Fatalf("retry should converge to success, got %s (%v)", res.Status, res.Err)
	}
	if recs, _ := env.svc.ops().List(); len(recs) != 0 {
		t.Fatalf("no records should remain after convergent retry, got %d", len(recs))
	}
	if ws, _ := env.svc.State.GetWorkspace("conv"); ws == nil {
		t.Fatal("workspace should exist after convergent retry")
	}
}

// TestCreateRetainsSourceReposOnFailure proves compensation never deletes source
// repositories (an acquired/cloned source is intentionally retained even when a
// later workspace mutation fails).
func TestCreateRetainsSourceReposOnFailure(t *testing.T) {
	env := setupTestEnv(t)
	api := env.createRepo("api")
	web := env.createRepo("web")
	fb := installCreateFaultBackend(env)
	fb.failBefore["WorktreeAdd:"+env.repoMap["web"]] = errors.New("web fails")

	res := env.svc.CreateWithResult("retain", CreateOpts{
		Branch: "feat/r", Repos: []string{"api", "web"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if res.Status != OutcomeFailed {
		t.Fatalf("expected failed, got %s", res.Status)
	}
	// Source repos must survive compensation.
	for _, src := range []string{api, web} {
		if _, err := os.Stat(src); err != nil {
			t.Fatalf("source repo %s must be retained: %v", src, err)
		}
	}
}

// TestCreateSkippedReposAppearInResult proves every requested repository appears
// in the ordered result even when an earlier repo fails.
func TestCreateSkippedReposAppearInResult(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createRepo("worker")
	fb := installCreateFaultBackend(env)
	fb.failBefore["WorktreeAdd:"+env.repoMap["api"]] = errors.New("api fails")

	res := env.svc.CreateWithResult("skips", CreateOpts{
		Branch: "feat/s", Repos: []string{"api", "web", "worker"}, RepoMap: env.repoMap, Cfg: env.cfg,
	})
	if len(res.Repos) != 3 {
		t.Fatalf("all 3 repos must appear in result, got %d: %+v", len(res.Repos), res.Repos)
	}
	if res.Repos[0].Status != state.RepoFailed {
		t.Fatalf("api should be failed: %+v", res.Repos[0])
	}
	if res.Repos[1].Status != state.RepoSkipped || res.Repos[2].Status != state.RepoSkipped {
		t.Fatalf("web/worker should be skipped: %+v", res.Repos[1:])
	}
}
