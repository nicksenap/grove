package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
)

// provisionOutcome records what provisioning a single repo actually did, so
// compensation touches only operation-created resources.
type provisionOutcome struct {
	rw                models.RepoWorktree
	branchOwnership   state.ResourceOwnership
	worktreeOwnership state.ResourceOwnership
	mode              state.ProvisionMode
	baseBranch        string
}

// CreateWithResult creates a workspace transactionally and returns an ordered,
// typed outcome. It writes a recovery record before the first Git/filesystem
// mutation, provisions every repo (tracking resource ownership), commits state
// exactly once under the state lock, and on any failure compensates in reverse
// order — deleting only operation-created branches/worktrees and preserving
// pre-existing ones. If compensation cannot complete, the recovery record is
// retained and the result is Pending.
func (s *Service) CreateWithResult(name string, opts CreateOpts) *OperationResult {
	res := &OperationResult{Kind: state.OpCreate, Workspace: name}

	// 1. Validate inputs before any mutation or optional cloning.
	sourcePaths, err := s.validateCreateInputs(name, opts)
	if err != nil {
		return res.fail(err)
	}

	// Reserve this workspace name for the whole critical section (reconcile,
	// provision, commit, compensate) so two same-name creates cannot corrupt a
	// shared root or each other's recovery journal. Different names still run
	// concurrently, and the global state lock is only taken briefly at commit.
	release, err := s.State.AcquireWorkspaceLock(context.Background(), name)
	if err != nil {
		return res.fail(err)
	}
	released := false
	releaseOnce := func() {
		if !released {
			released = true
			release()
		}
	}
	defer releaseOnce()

	// Re-check duplicate now that we hold the reservation, but resume a stale
	// record FIRST so a crash after a prior commit is reconciled (record cleared)
	// rather than masked by a bare "already exists".
	if err := s.resumeStaleCreate(name); err != nil {
		return res.fail(fmt.Errorf("resolving prior interrupted create: %w", err))
	}
	if existing, err := s.State.GetWorkspace(name); err != nil {
		return res.fail(err)
	} else if existing != nil {
		return res.fail(fmt.Errorf("workspace %s already exists", name))
	}

	wsPath := filepath.Join(opts.Cfg.WorkspaceDir, name)

	// 3. Write the recovery record before the first Git/filesystem mutation.
	rec := newCreateRecord(name, wsPath, opts, sourcePaths)
	if err := s.ops().Write(rec); err != nil {
		return res.fail(fmt.Errorf("writing recovery record: %w", err))
	}

	logging.Info("creating workspace %q (branch=%s, repos=%v)", name, opts.Branch, opts.Repos)

	// 4. Create the workspace root using write-ahead ownership: record whether we
	//    intend to own the root BEFORE mkdir, so a crash mid-create is recoverable.
	rootOwnership := state.OwnCreated
	if _, err := os.Stat(wsPath); err == nil {
		rootOwnership = state.OwnPreexisting
	}
	rec.RootOwnership = rootOwnership
	if err := s.ops().Write(rec); err != nil {
		_ = s.ops().Delete(rec.ID)
		return res.fail(fmt.Errorf("persisting recovery record: %w", err))
	}
	if err := s.mut().Mkdir(wsPath, 0o755); err != nil {
		// Reconcile a possible applied-then-error mkdir: compensate (idempotent)
		// then clear the record only if the root is gone.
		return s.compensateCreateResult(res, rec, wsPath, rootOwnership, nil,
			fmt.Errorf("creating workspace dir: %w", err))
	}

	// 5. Parallel fetch (outside the state lock — the slow network part).
	s.fetchRepos(opts.Repos, sourcePaths)

	// 6. Provision every repo; on failure this compensates and returns.
	outs, failed := s.provisionAll(res, rec, opts, sourcePaths, wsPath, rootOwnership)
	if failed != nil {
		return failed
	}

	// 7. Commit state exactly once under the lock (authoritative dup check).
	ws := models.NewWorkspace(name, wsPath, opts.Branch)
	ws.Source = opts.Source
	ws.Repos = collectWorktrees(outs)
	rec.Phase = "commit"
	rec.CommitStatus = state.CommitAttempted
	if err := s.ops().Write(rec); err != nil {
		// Do not commit if we cannot durably record the attempt.
		return s.compensateCreateResult(res, rec, wsPath, rootOwnership, outs,
			fmt.Errorf("persisting commit intent: %w", err))
	}

	if commitErr := s.withMutation(context.Background(), func(m *state.Mutation) error {
		return m.Add(ws)
	}); commitErr != nil {
		// A genuine same-name conflict means another workspace owns the name; our
		// provisioned resources must be compensated.
		if state.CodeOf(commitErr) == state.CodeStateConflict {
			return s.compensateCreateResult(res, rec, wsPath, rootOwnership, outs, commitErr)
		}
		// Otherwise the commit may have applied then returned an error (ambiguous,
		// e.g. a directory-sync failure after rename). Reconcile against
		// authoritative state while still holding the per-workspace reservation.
		return s.reconcileAmbiguousCommit(res, rec, ws, wsPath, rootOwnership, outs, commitErr, releaseOnce)
	}
	rec.CommitStatus = state.CommitDone
	_ = s.ops().Write(rec)

	return s.finishCreateSuccess(res, rec, ws, wsPath, releaseOnce)
}

// reconcileAmbiguousCommit handles a state-commit that returned an error but may
// have applied. It verifies workspace identity under the reservation: a matching
// present workspace is treated as committed; a read error or identity mismatch
// retains the CommitAttempted record as Pending; a confirmed absence compensates.
func (s *Service) reconcileAmbiguousCommit(res *OperationResult, rec *state.OperationRecord, ws models.Workspace,
	wsPath string, rootOwnership state.ResourceOwnership, outs []*provisionOutcome, cause error, releaseOnce func()) *OperationResult {

	existing, err := s.State.GetWorkspace(ws.Name)
	if err != nil {
		// Cannot prove state; keep the record for reconciliation rather than risk
		// destroying a committed workspace.
		rec.LastError = fmt.Sprintf("ambiguous commit; state unreadable: %v", cause)
		rec.Retryable = true
		_ = s.ops().Write(rec)
		res.Status = OutcomePending
		res.RecordID = rec.ID
		res.Err = cause
		res.Message = "state commit ambiguous and unreadable — run: gw doctor"
		return res
	}
	if existing != nil && workspaceMatches(existing, ws) {
		rec.CommitStatus = state.CommitDone
		_ = s.ops().Write(rec)
		return s.finishCreateSuccess(res, rec, ws, wsPath, releaseOnce)
	}
	if existing != nil {
		// A different workspace holds the name; ours did not commit — compensate.
		return s.compensateCreateResult(res, rec, wsPath, rootOwnership, outs, cause)
	}
	return s.compensateCreateResult(res, rec, wsPath, rootOwnership, outs, cause)
}

// workspaceMatches reports whether the persisted workspace matches the one this
// operation intended to commit (path, branch, and full repo identity).
func workspaceMatches(got *models.Workspace, want models.Workspace) bool {
	if got.Path != want.Path || got.Branch != want.Branch || len(got.Repos) != len(want.Repos) {
		return false
	}
	type ident struct{ src, wt, br string }
	have := map[string]ident{}
	for _, r := range got.Repos {
		have[r.RepoName] = ident{r.SourceRepo, r.WorktreePath, r.Branch}
	}
	for _, r := range want.Repos {
		got, ok := have[r.RepoName]
		if !ok || got.src != r.SourceRepo || got.wt != r.WorktreePath || got.br != r.Branch {
			return false
		}
	}
	return true
}

// finishCreateSuccess clears the record, releases the workspace reservation, and
// runs post-commit best-effort steps (stats, mcp, and setup hooks). Setup hooks
// run AFTER the reservation is released so a hook that touches the same
// workspace cannot self-block.
func (s *Service) finishCreateSuccess(res *OperationResult, rec *state.OperationRecord, ws models.Workspace, wsPath string, releaseOnce func()) *OperationResult {
	if err := s.ops().Delete(rec.ID); err != nil {
		logging.Warn("workspace %q created but recovery record %s could not be cleared: %v", ws.Name, rec.ID, err)
	}

	s.Stats.RecordCreated(ws)
	writeMCPConfig(ws)
	logging.Info("workspace %q created at %s", ws.Name, wsPath)
	console.Successf("Workspace %s created at %s", ws.Name, wsPath)
	if cdFile := os.Getenv("GROVE_CD_FILE"); cdFile != "" {
		_ = os.WriteFile(cdFile, []byte(wsPath), 0o644)
	}

	// Release the reservation before setup hooks (hooks run after commit and
	// outside the lock, per the lifecycle contract).
	releaseOnce()
	if hookErr := s.runSetupHooksErr(ws); hookErr != nil {
		res.Status = OutcomePartial
		res.Err = hookErr
		res.Message = "workspace created, but setup hooks failed: " + hookErr.Error()
		return res
	}
	res.Status = OutcomeSuccess
	res.Message = fmt.Sprintf("Workspace %s created at %s", ws.Name, wsPath)
	return res
}

// provisionAll provisions each repo sequentially. On the first failure it
// records the outcome, marks later repos skipped, compensates, and returns a
// non-nil terminal result.
func (s *Service) provisionAll(res *OperationResult, rec *state.OperationRecord, opts CreateOpts,
	sourcePaths []string, wsPath string, rootOwnership state.ResourceOwnership) ([]*provisionOutcome, *OperationResult) {

	var outs []*provisionOutcome
	for i, repoName := range opts.Repos {
		console.Infof("[%d/%d] %s", i+1, len(opts.Repos), repoName)
		mode := opts.BranchMode
		if opts.TrackBranchRepo != "" && repoName != opts.TrackBranchRepo {
			mode = BranchModeCreate
		}
		out, err := s.provisionRepo(rec, i, sourcePaths[i], repoName, wsPath, opts.Branch, mode)
		if out != nil {
			outs = append(outs, out)
		}
		if err != nil {
			if out != nil {
				rec.Repos[i] = repoOpFromOutcome(repoName, out)
			}
			rec.Repos[i].Status = state.RepoFailed
			rec.Repos[i].Error = err.Error()
			res.addRepo(RepoOutcome{RepoName: repoName, Status: state.RepoFailed, Phase: "provision", Err: err})
			// Every requested repository must appear in the result; later repos are
			// skipped (not attempted).
			for j := i + 1; j < len(opts.Repos); j++ {
				rec.Repos[j].Status = state.RepoSkipped
				res.addRepo(RepoOutcome{RepoName: opts.Repos[j], Status: state.RepoSkipped})
			}
			return outs, s.compensateCreateResult(res, rec, wsPath, rootOwnership, outs,
				fmt.Errorf("provisioning %s: %w", repoName, err))
		}
		rec.Repos[i] = repoOpFromOutcome(repoName, out)
		if werr := s.ops().Write(rec); werr != nil {
			// A failed progress write means the durable journal no longer reflects
			// reality; stop and compensate rather than continue with stale WAL.
			res.addRepo(RepoOutcome{RepoName: repoName, Status: state.RepoDone, Phase: "provision"})
			for j := i + 1; j < len(opts.Repos); j++ {
				rec.Repos[j].Status = state.RepoSkipped
				res.addRepo(RepoOutcome{RepoName: opts.Repos[j], Status: state.RepoSkipped})
			}
			return outs, s.compensateCreateResult(res, rec, wsPath, rootOwnership, outs,
				fmt.Errorf("persisting recovery record: %w", werr))
		}
		res.addRepo(RepoOutcome{RepoName: repoName, Status: state.RepoDone, Phase: "provision"})
	}
	return outs, nil
}

// validateCreateInputs validates inputs and resolves source paths, mutating
// nothing. It performs a non-authoritative early duplicate check for UX; the
// authoritative check happens under the lock at commit.
func (s *Service) validateCreateInputs(name string, opts CreateOpts) ([]string, error) {
	if err := ValidateWorkspaceName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.Branch) == "" {
		return nil, errors.New("branch is required")
	}
	if len(opts.Repos) == 0 {
		return nil, errors.New("at least one repo is required")
	}
	if opts.Cfg == nil {
		return nil, errors.New("config is required")
	}
	sourcePaths := make([]string, len(opts.Repos))
	for i, repoName := range opts.Repos {
		sp, ok := opts.RepoMap[repoName]
		if !ok {
			return nil, fmt.Errorf("repo %s not found", repoName)
		}
		sourcePaths[i] = sp
	}
	if existing, err := s.State.GetWorkspace(name); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("workspace %s already exists", name)
	}
	return sourcePaths, nil
}

// newCreateRecord builds the initial create recovery record.
func newCreateRecord(name, wsPath string, opts CreateOpts, sourcePaths []string) *state.OperationRecord {
	rec := &state.OperationRecord{
		Kind:      state.OpCreate,
		Workspace: name,
		Path:      wsPath,
		Source:    opts.Source,
		Phase:     "provisioning",
	}
	for i, repoName := range opts.Repos {
		rec.Repos = append(rec.Repos, state.RepoOperation{
			RepoName:   repoName,
			SourceRepo: sourcePaths[i],
			Branch:     opts.Branch,
			Status:     state.RepoPending,
		})
	}
	return rec
}

// provisionRepo provisions one repo's worktree via the mutation backend using
// write-ahead ownership: it records the ownership it is about to take and the
// current phase in the recovery journal BEFORE each Git mutation, so a crash
// mid-mutation still leaves a durable record of what may have been created.
// Compensation (which checks actual existence) can then clean up exactly.
func (s *Service) provisionRepo(rec *state.OperationRecord, idx int, sourcePath, repoName, wsPath, branch string, mode BranchMode) (*provisionOutcome, error) {
	wtPath := filepath.Join(wsPath, repoName)
	out := &provisionOutcome{
		rw: models.RepoWorktree{RepoName: repoName, SourceRepo: sourcePath, WorktreePath: wtPath, Branch: branch},
	}

	// Serialize branch ownership for this exact (source repo, branch) pair so two
	// creates for DIFFERENT workspace names that target the same branch cannot
	// both classify it as operation-created and later delete each other's branch.
	relBranch, err := s.State.AcquireResourceLock(context.Background(), "branch-"+sourcePath+"-"+branch)
	if err != nil {
		return out, err
	}
	defer relBranch()

	if hasWT, _ := gitops.WorktreeHasBranch(sourcePath, branch); hasWT {
		return out, fmt.Errorf("branch %s already has a worktree in %s", branch, repoName)
	}

	// Track mode: check out an existing remote branch (e.g. a PR head).
	if mode == BranchModeTrack && !gitops.BranchExists(sourcePath, branch) {
		if gitops.RemoteBranchExists(sourcePath, branch) {
			logging.Info("tracking existing remote branch %q in %s", branch, repoName)
			// Write-ahead: tracking creates both a local branch and a worktree.
			out.mode = state.ProvisionTrack
			out.branchOwnership = state.OwnCreated
			out.worktreeOwnership = state.OwnCreated
			if err := s.writeAhead(rec, idx, out, "track"); err != nil {
				return out, err
			}
			if err := s.mut().WorktreeAddTracking(sourcePath, wtPath, branch); err != nil {
				return out, fmt.Errorf("adding tracking worktree: %w", err)
			}
			return out, nil
		}
		console.Warningf("%s: remote branch %s not found — creating a new branch from base instead", repoName, branch)
	}

	// Branch ownership: only compensate a branch this operation creates.
	if gitops.BranchExists(sourcePath, branch) {
		out.branchOwnership = state.OwnPreexisting
	} else {
		base, err := gitops.ResolveBaseBranch(sourcePath)
		if err != nil {
			base = "HEAD"
		}
		out.baseBranch = base
		// Write-ahead: record that we intend to own this branch before creating it.
		out.branchOwnership = state.OwnCreated
		if err := s.writeAhead(rec, idx, out, "branch_create"); err != nil {
			return out, err
		}
		if err := s.createBranchWithFallback(sourcePath, repoName, branch, base); err != nil {
			return out, fmt.Errorf("creating branch: %w", err)
		}
	}

	// Write-ahead: record intended worktree ownership before adding it.
	out.worktreeOwnership = state.OwnCreated
	if err := s.writeAhead(rec, idx, out, "worktree_add"); err != nil {
		return out, err
	}
	if err := s.mut().WorktreeAdd(sourcePath, wtPath, branch); err != nil {
		return out, fmt.Errorf("adding worktree: %w", err)
	}
	return out, nil
}

// writeAhead persists the repo's intended ownership/phase before a mutation.
func (s *Service) writeAhead(rec *state.OperationRecord, idx int, out *provisionOutcome, phase string) error {
	op := repoOpFromOutcome(out.rw.RepoName, out)
	op.Status = state.RepoInProgress
	op.Phase = phase
	rec.Repos[idx] = op
	if err := s.ops().Write(rec); err != nil {
		return fmt.Errorf("persisting recovery record: %w", err)
	}
	return nil
}

// createBranchWithFallback reproduces the historical base → plain → HEAD
// fallback for branch creation via the mutation backend.
func (s *Service) createBranchWithFallback(sourcePath, repoName, branch, base string) error {
	logging.Info("creating branch %q in %s from %s", branch, repoName, base)
	if err := s.mut().CreateBranch(sourcePath, branch, base); err == nil {
		return nil
	}
	plainBase := strings.TrimPrefix(base, "origin/")
	if err := s.mut().CreateBranch(sourcePath, branch, plainBase); err == nil {
		return nil
	}
	return s.mut().CreateBranch(sourcePath, branch, "HEAD")
}

// compensateCreateResult rolls back provisioned resources in reverse order and
// finalizes the result as Failed (fully rolled back) or Pending (rollback
// incomplete — recovery record retained).
func (s *Service) compensateCreateResult(res *OperationResult, rec *state.OperationRecord, wsPath string,
	rootOwnership state.ResourceOwnership, outs []*provisionOutcome, cause error) *OperationResult {

	logging.Error("workspace creation failed for %q: %v — rolling back", rec.Workspace, cause)
	cerrs := s.compensateCreate(wsPath, rootOwnership, outs)

	res.Err = cause
	if len(cerrs) == 0 {
		if err := s.ops().Delete(rec.ID); err != nil {
			logging.Warn("rollback complete but recovery record %s not cleared: %v", rec.ID, err)
		}
		res.Status = OutcomeFailed
		res.Message = "creation failed and was rolled back: " + cause.Error()
		return res
	}

	rec.Phase = "compensation"
	rec.Retryable = true
	rec.LastError = joinErrors(cause, cerrs)
	_ = s.ops().Write(rec)
	res.Status = OutcomePending
	res.RecordID = rec.ID
	res.Message = "creation failed; automatic rollback incomplete — run: gw doctor"
	return res
}

// compensateCreate removes only operation-created resources, in reverse order,
// aggregating (not dropping) errors. It is idempotent: a resource marked as
// owned via write-ahead but never actually created (e.g. a crash before the Git
// call applied) is simply skipped, so retry converges.
func (s *Service) compensateCreate(wsPath string, rootOwnership state.ResourceOwnership, outs []*provisionOutcome) []error {
	var errs []error
	for i := len(outs) - 1; i >= 0; i-- {
		o := outs[i]
		if o == nil {
			continue
		}
		if o.worktreeOwnership == state.OwnCreated {
			if err := s.compensateWorktree(o.rw); err != nil {
				errs = append(errs, err)
			}
		}
		if o.branchOwnership == state.OwnCreated {
			if gitops.BranchExists(o.rw.SourceRepo, o.rw.Branch) {
				if err := s.mut().DeleteBranch(o.rw.SourceRepo, o.rw.Branch, true); err != nil {
					// Only a still-present branch is a real failure.
					if gitops.BranchExists(o.rw.SourceRepo, o.rw.Branch) {
						errs = append(errs, fmt.Errorf("%s: branch delete: %w", o.rw.RepoName, err))
					}
				}
			}
		}
	}
	if rootOwnership == state.OwnCreated {
		if _, err := os.Stat(wsPath); err == nil || !os.IsNotExist(err) {
			if rmErr := s.mut().RemoveAll(wsPath); rmErr != nil {
				if _, st := os.Stat(wsPath); st == nil || !os.IsNotExist(st) {
					errs = append(errs, fmt.Errorf("workspace root: %w", rmErr))
				}
			}
		}
	}
	return errs
}

// compensateWorktree removes an operation-created worktree, reconciling against
// BOTH the filesystem and the Git worktree registration (a directory can be
// deleted while its worktree registration lingers as prunable). A registration
// that cannot be inspected is treated as still-present (returns an error so the
// caller keeps the record pending).
func (s *Service) compensateWorktree(rw models.RepoWorktree) error {
	registered, listErr := s.worktreeRegistered(rw.SourceRepo, rw.WorktreePath)
	if listErr != nil {
		return fmt.Errorf("%s: worktree registration inspect: %w", rw.RepoName, listErr)
	}
	_, statErr := os.Stat(rw.WorktreePath)
	exists := statErr == nil || !os.IsNotExist(statErr)

	switch {
	case !registered && !exists:
		return nil // nothing to do (write-ahead resource never actually created)
	case registered:
		// `git worktree remove --force` handles both the registration and the
		// directory, and works even when the directory is already gone. It is
		// path-scoped, so it never touches unrelated prunable registrations.
		_ = s.mut().WorktreeRemove(rw.SourceRepo, rw.WorktreePath)
	case exists:
		// Directory present but not a registered worktree (e.g. mkdir'd then add
		// failed): remove the directory.
		_ = s.mut().RemoveAll(rw.WorktreePath)
	}

	// Verify convergence: only a still-present worktree/registration is a failure.
	reg, listErr := s.worktreeRegistered(rw.SourceRepo, rw.WorktreePath)
	if listErr != nil {
		return fmt.Errorf("%s: worktree registration inspect: %w", rw.RepoName, listErr)
	}
	if _, st := os.Stat(rw.WorktreePath); reg || st == nil || !os.IsNotExist(st) {
		return fmt.Errorf("%s: worktree still present after removal", rw.RepoName)
	}
	return nil
}

// worktreeRegistered reports whether path is a registered worktree of repo.
func (s *Service) worktreeRegistered(repo, path string) (bool, error) {
	entries, err := gitops.WorktreeList(repo)
	if err != nil {
		return false, err
	}
	want := resolvePath(path)
	for _, e := range entries {
		if resolvePath(e.Path) == want {
			return true, nil
		}
	}
	return false, nil
}

// resolvePath canonicalizes a path for comparison (symlinks + cleaning). When
// the leaf no longer exists (e.g. a deleted worktree directory), it resolves the
// parent directory's symlinks and re-appends the base, so a stale registration
// recorded under a resolved path (/private/var/...) still matches a caller path
// expressed via a symlinked prefix (/var/...).
func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(r)
	}
	parent := filepath.Dir(p)
	if rp, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Clean(filepath.Join(rp, filepath.Base(p)))
	}
	return filepath.Clean(p)
}

// resumeStaleCreate conservatively resolves a create record left by a previous
// crashed run for the same workspace name. If state already contains the
// workspace, the earlier commit succeeded and the record is simply cleared.
// Otherwise the record's operation-created resources are compensated using the
// recorded ownership, then the record is removed so a fresh create can proceed.
func (s *Service) resumeStaleCreate(name string) error {
	recs, err := s.ops().List()
	if err != nil {
		return err
	}
	for i := range recs {
		rec := recs[i]
		if rec.Kind != state.OpCreate || rec.Workspace != name {
			continue
		}
		if !rec.Supported() {
			return fmt.Errorf("unsupported prior create record %s; run gw doctor", rec.ID)
		}
		ws, err := s.State.GetWorkspace(name)
		if err != nil {
			return err
		}
		if ws != nil {
			// Prior commit succeeded; just clear the stale record.
			if err := s.ops().Delete(rec.ID); err != nil {
				return err
			}
			continue
		}
		// Compensate operation-created resources from the record. If the record
		// carries any unknown-ownership resource that might still exist, retain it
		// rather than discarding the only repair evidence.
		if recordHasUnknownOwnership(&rec) {
			return fmt.Errorf("prior interrupted create for %q has unknown resource ownership; run gw doctor", name)
		}
		outs := outcomesFromRecord(&rec)
		if cerrs := s.compensateCreate(rec.Path, rec.RootOwnership, outs); len(cerrs) > 0 {
			return fmt.Errorf("could not roll back prior interrupted create: %s", joinErrors(nil, cerrs))
		}
		if err := s.ops().Delete(rec.ID); err != nil {
			return err
		}
	}
	return nil
}

// recordHasUnknownOwnership reports whether any operation-created resource in
// the record has undetermined ownership (so compensation cannot be exact).
func recordHasUnknownOwnership(rec *state.OperationRecord) bool {
	for _, r := range rec.Repos {
		// A repo that was attempted (in-progress or failed) but whose ownership was
		// never determined cannot be compensated exactly.
		if r.Status != state.RepoFailed && r.Status != state.RepoInProgress {
			continue
		}
		if r.BranchOwnership == state.OwnUnknown && r.Branch != "" {
			return true
		}
		if r.WorktreeOwnership == state.OwnUnknown && r.WorktreePath != "" {
			return true
		}
	}
	return false
}
func (s *Service) fetchRepos(repoNames, sourcePaths []string) {
	console.Infof("fetching %d repos...", len(repoNames))
	var wg sync.WaitGroup
	for i := range repoNames {
		wg.Add(1)
		go func(source, name string) {
			defer wg.Done()
			if err := gitops.Fetch(source); err != nil {
				console.Warningf("  %s: fetch failed, using local state", name)
			}
		}(sourcePaths[i], repoNames[i])
	}
	wg.Wait()
}

// runSetupHooksErr runs setup hooks and returns an aggregated error (nil if all
// succeed or there are no hooks).
func (s *Service) runSetupHooksErr(ws models.Workspace) error {
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	for _, r := range ws.Repos {
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg == nil || len(groveCfg.Setup) == 0 {
			continue
		}
		wg.Add(1)
		go func(repo models.RepoWorktree, cmds []string) {
			defer wg.Done()
			for _, cmdStr := range cmds {
				if err := s.RunCmd(repo.WorktreePath, cmdStr); err != nil {
					console.Warningf("setup hook failed in %s: %s", repo.RepoName, err)
					mu.Lock()
					errs = append(errs, fmt.Errorf("%s: %w", repo.RepoName, err))
					mu.Unlock()
				}
			}
		}(r, []string(groveCfg.Setup))
	}
	wg.Wait()
	if len(errs) == 0 {
		return nil
	}
	return joinErrs(errs)
}

// --- helpers ---

func repoOpFromOutcome(name string, out *provisionOutcome) state.RepoOperation {
	return state.RepoOperation{
		RepoName:          name,
		SourceRepo:        out.rw.SourceRepo,
		WorktreePath:      out.rw.WorktreePath,
		Branch:            out.rw.Branch,
		BaseBranch:        out.baseBranch,
		Mode:              out.mode,
		Status:            state.RepoDone,
		BranchOwnership:   out.branchOwnership,
		WorktreeOwnership: out.worktreeOwnership,
	}
}

func outcomesFromRecord(rec *state.OperationRecord) []*provisionOutcome {
	var outs []*provisionOutcome
	for _, r := range rec.Repos {
		// Include done, failed, and in-progress repos: any of them may have created
		// resources (compensation checks actual existence and is idempotent).
		if r.Status == state.RepoPending || r.Status == state.RepoSkipped {
			continue
		}
		outs = append(outs, &provisionOutcome{
			rw: models.RepoWorktree{
				RepoName:     r.RepoName,
				SourceRepo:   r.SourceRepo,
				WorktreePath: r.WorktreePath,
				Branch:       r.Branch,
			},
			branchOwnership:   r.BranchOwnership,
			worktreeOwnership: r.WorktreeOwnership,
			mode:              r.Mode,
			baseBranch:        r.BaseBranch,
		})
	}
	return outs
}

func collectWorktrees(outs []*provisionOutcome) []models.RepoWorktree {
	rws := make([]models.RepoWorktree, 0, len(outs))
	for _, o := range outs {
		rws = append(rws, o.rw)
	}
	return rws
}

func joinErrors(cause error, errs []error) string {
	parts := make([]string, 0, len(errs)+1)
	if cause != nil {
		parts = append(parts, cause.Error())
	}
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}

func joinErrs(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return errors.New(strings.Join(msgs, "; "))
}

// ValidateWorkspaceName rejects names that would escape the workspace dir or
// collide with path handling.
func ValidateWorkspaceName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("workspace name is required")
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid workspace name %q", name)
	}
	if name != filepath.Base(name) {
		return fmt.Errorf("invalid workspace name %q", name)
	}
	return nil
}

// fail marks the result as a precondition failure (nothing mutated) and returns it.
func (r *OperationResult) fail(err error) *OperationResult {
	r.Status = OutcomeFailed
	r.Err = err
	r.Message = err.Error()
	return r
}
