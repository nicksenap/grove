# Implementation Plan: Transactional and Recoverable Workspace Operations

**Target:** [GitHub issue #59](https://github.com/nicksenap/grove/issues/59)  
**Status:** Approved for implementation; no implementation has started.  
**Related boundary:** [Issue #63](https://github.com/nicksenap/grove/issues/63) owns the public machine-output envelope and global `--format json` contract.

## Overview

Grove must keep `~/.grove/state.json`, workspace paths, Git worktree registrations, and workspace branches consistent across failures and concurrent `gw` processes. Each mutating operation will complete, compensate everything it created, or leave a durable recovery record that the original command and `gw doctor` can explain and repair.

This plan delivers complete operation paths rather than implementing all storage, all Git wrappers, then all CLI work. The two foundation tasks are limited to an end-to-end state mutation boundary and recovery-record lifecycle; each later task carries one user operation through service behavior, failure recovery, CLI exit handling, and tests.

## Scope

- Cross-process serialization and lost-update-safe state mutations.
- Durable, operation-specific recovery records for interrupted mutations.
- Transactional `create` and multi-repo `add-repo`.
- Safe, retryable `remove-repo` and `delete` with explicit destructive force.
- Aggregated per-repository and per-workspace outcomes with meaningful exits.
- Four-way reconciliation of state, filesystem, Git worktree registration, and branch state.
- Idempotent repair for every recovery-record type, including rename and sync cleanup.
- Fault-injection, race, deterministic e2e, and documentation coverage.

## Non-goals

- Distributed or cloud state.
- A general-purpose transaction framework.
- The public versioned CLI envelope/global machine mode; that belongs to issue #63.
- New task/PR intake, blueprint, Oven, MCP, or agent features.
- Hiding Git failures that require user action.
- Locking unrelated best-effort files such as `stats.json`.
- Redefining successful branch-cleanup ownership semantics beyond what safe compensation and repair require.

## Current Evidence

- State mutators are unlocked `Load` → edit → `Save` sequences (`internal/state/state.go`), so concurrent processes can lose updates.
- `Save` uses one shared `.tmp` path and does not implement the fsync guarantees claimed by OpenWiki.
- Create rollback removes worktrees but not newly created branches and drops rollback errors (`internal/workspace/workspace.go`).
- `AddRepos` validates and provisions incrementally without compensation.
- Worktree removal is always forced; delete/remove fall back to unchecked `os.RemoveAll` and suppress failures (`internal/gitops/gitops.go`, `internal/workspace/workspace.go`).
- Doctor only checks path existence and can discard repair evidence.
- Sync reports repository failures to the console but returns `nil`; rename ignores worktree-repair failures.
- Existing real-Git fixtures are useful, but current rollback and partial-delete tests do not inject post-mutation failures.
- Planning baseline: `go test ./...` and `go test -race ./internal/state ./internal/workspace` pass. This does not exercise cross-process lost updates.

## Conditional Lifecycle Contract for Human Approval

The task criteria below assume approval of this contract. If any item changes, update the affected tasks before implementation.

1. **Mutation ownership API:** `Store.WithMutation(ctx, func(*state.Mutation) error)` acquires one stable advisory lock, loads the authoritative snapshot, and passes a caller-owned handle with locked `Get/Add/Update/Remove`, recovery-record CRUD, and `Commit` methods. Service code never calls a lock-acquiring public mutator while holding this handle. Raw full-snapshot `Save` becomes internal or revision-checked so stale snapshots cannot overwrite newer state.
2. **Lock behavior:** wait up to 30 seconds, then return retryable `STATE_LOCK_TIMEOUT`; same-name/same-workspace conflicts are revalidated under the lock and return deterministic conflict outcomes. The lock file is never unlinked.
3. **Durable state writes:** use a unique temp file in the destination directory, sync it, atomically rename it, and sync the parent directory.
4. **Small recovery journal:** versioned, operation-specific records live under `~/.grove/operations/`. A record is written before the first **workspace** Git/filesystem mutation, captures repository phases/resource ownership, and is removed only after state commit or complete compensation. Supported kinds are create, add, remove, delete, sync cleanup, and rename.
5. **Source cloning boundary:** remote-URL cloning is source-repository acquisition, not workspace mutation. Validate command inputs before cloning; successful clones are intentionally retained as reusable local sources if later workspace creation/addition fails. Clone failures are reported as preflight failures and tested, but clones are not compensated.
6. **Typed service outcomes:** issue #59 introduces ordered `OperationResult`/`RepoOutcome` values and stable internal error codes. Human CLI commands render all repository details to stderr and return non-zero on failed/partial/pending outcomes. Public machine envelopes/`--format json` are deferred to issue #63. Existing `doctor --json` remains compatible and gains additive stable diagnosis/repair fields required by #59.
7. **Force behavior:** add `--yes/-y` for confirmation-only bypass. `--force/-f` implies yes and authorizes dirty-worktree or validated filesystem fallback. Force never bypasses canonical workspace-boundary, source identity, or expected worktree-path checks.
8. **Hook boundary:** global `pre_delete` and per-repo teardown run outside the state lock; the service acquires the lock and repeats complete identity/registration/dirty preflight immediately afterward. An aborting global hook stops that workspace; warning hooks are included in aggregate outcomes. Teardown failure blocks that repository and remains retryable. Setup/post-create run after commit/lock release; failure yields a partial/non-zero outcome without undoing a valid workspace.
9. **Exit behavior:** exit 0 only for complete success or explicit cancellation. Precondition failure, partial success, cleanup pending, and repair failure exit non-zero after all requested targets are attempted.
10. **Doctor behavior:** existing `doctor --json` stays a bare array for compatibility. Diagnosis reports recovery records from the first lifecycle slice onward. `doctor --fix` applies versioned actions with precondition checks; actionable diagnosis and repair failures return non-zero.

## Dependency Graph

```text
Locked Mutation handle + durable writer
                    │
                    ▼
Recovery records + injected mutation seams + doctor visibility
                    │
          ┌─────────┴─────────┐
          ▼                   ▼
Transactional create     Safe remove preflight
          │                   │
          ▼                   ▼
Atomic add-repo      Retryable remove-repo
                              │
                              ▼
                     Retryable workspace delete
                              │
                              ▼
                    Multi-workspace aggregation
          └──────────────┬──────────────┘
                         ▼
                Four-way doctor plan
                  ┌──────┴──────┐
                  ▼             ▼
       Constructive repair   Destructive repair
                  └──────┬──────┘
                         ▼
              Sync + rename recovery
                         │
                         ▼
                Race/e2e gates + docs
```

## Verification Convention

Each implementation task names the exact tests it must add. Before running a targeted command, verify they exist with `go test <pkg> -list '<anchored pattern>' | grep -q .`; a zero-test `go test -run` is not acceptance evidence. Fault injection must be instance-scoped/test-only; filesystem-permission tricks are not the primary seam.

---

## Phase 1: State and Recovery Foundations

### Task 1: Serialize state mutations across processes

**Description:** Implement one complete lock-backed state mutation path from lock acquisition through authoritative read, revision-safe edits, durable commit, and release. Migrate public Store mutators to wrappers over that path without allowing nested acquisition.

**Acceptance criteria:**
- [ ] `Mutation` owns locked reads/edits/commit; raw stale snapshot replacement cannot overwrite a newer revision.
- [ ] Concurrent helper processes preserve independent updates; a same-record conflict has one deterministic winner and one conflict result.
- [ ] Callback/write/sync/rename/process-death failures preserve the last valid JSON, release the lock, and leave no shared temp collision.

**Verification:**
- [ ] Add and run `TestStoreConcurrentSubprocessMutations`, `TestStoreMutationFailurePreservesState`, and `TestStateLockReleasedAfterProcessExit` repeatedly under `-race`.
- [ ] Cross-compile the state package/test binary for released Darwin/Linux targets.

**Dependencies:** None  
**Files likely touched:** `internal/state/state.go`, `internal/state/state_test.go`, new `internal/state/lock_unix.go`, `go.mod`, `go.sum`  
**Estimated scope:** Medium (4–5 files)

### Task 2: Persist and surface recovery records

**Description:** Add the fixed recovery-record schema/store, instance-scoped Git/filesystem/state fault seams, and generic doctor visibility. This task proves a record can survive interruption and be reported before any operation-specific mutation is migrated.

**Acceptance criteria:**
- [ ] Records for create/add/remove/delete/sync/rename atomically capture intent, phase, resource ownership, ordered repository outcomes, and the latest retryable error.
- [ ] The workspace service receives a narrow injectable mutation backend; release binaries cannot enable test failpoints.
- [ ] `gw doctor` reports incomplete/stale records without mutation, and interrupted helper-process records survive restart.

**Verification:**
- [ ] Add and run `TestOperationRecordRoundTrip`, `TestOperationRecordSurvivesProcessExit`, and `TestDoctorReportsRecoveryRecord`.
- [ ] Confirm old/no-operations state loads unchanged and no temporary record files remain after successful completion.

**Dependencies:** Task 1  
**Files likely touched:** new `internal/state/operation.go`, operation tests, `internal/workspace/service.go`, a focused workspace fault-test helper, `internal/workspace/workspace.go` or new doctor helper  
**Estimated scope:** Medium (4–5 files)

## Checkpoint: State Foundation (after Tasks 1–2)

- [ ] Concurrent state updates are lossless across processes.
- [ ] Recovery intent and test seams exist before lifecycle work begins.
- [ ] Doctor can identify a stranded record.
- [ ] Existing state fixtures still load unchanged.
- [ ] Human approves the conditional lifecycle contract before Task 3.

---

## Phase 2: Constructive Operations

### Task 3: Make workspace creation transactional and recoverable

**Description:** Carry create from validated CLI inputs through source resolution, operation record, authoritative duplicate check, branch/worktree provisioning, one state commit, reverse-order compensation, original-command retry, and typed CLI outcome.

**Acceptance criteria:**
- [ ] Validate branch/name/repo inputs before optional cloning; retained-clone behavior is explicit. Failures at mkdir, branch create, worktree add, a later repo, or state commit fully compensate or remain recoverable.
- [ ] Compensation deletes only operation-created resources, preserves pre-existing branches, aggregates rollback errors, and handles track mode.
- [ ] Success aligns state/path/registration/branch and clears the record; `gw create` can safely resume/finish its own pending record and reports setup/post-create failures as partial after commit.

**Verification:**
- [ ] Add exact real-Git/fault tests for branch-before-worktree failure, later-repo failure, final commit failure, rollback failure, track mode, retained clone, and same-name concurrency.
- [ ] Add command tests proving all repo outcomes render and partial/pending create exits non-zero.

**Dependencies:** Tasks 1–2  
**Files likely touched:** `internal/workspace/service.go`, `internal/workspace/workspace.go` or new `internal/workspace/create.go`, new `internal/workspace/result.go`, workspace tests, `cmd/create.go`  
**Estimated scope:** Medium (4–5 files)

### Task 4: Make multi-repository additions atomic and recoverable

**Description:** Apply the constructive transaction to `add-repo`: validate all inputs, retain explicitly acquired clones, provision every requested repo, commit once, compensate all additions on failure, and resume a pending add safely.

**Acceptance criteria:**
- [ ] Invalid/later failing input produces no committed addition and no operation-created branch/worktree leak; retained source clones are documented outcomes.
- [ ] Final state-update failure restores the original workspace exactly or leaves a record that `gw add-repo` and doctor can explain/retry.
- [ ] Duplicate/already-present inputs are deterministic, setup runs only after commit, and every requested repository appears in the typed result.

**Verification:**
- [ ] Add exact tests for later unknown repo preflight, repo-2 branch/worktree failure, commit failure, rollback failure, retained clone, retry, and idempotent duplicates.
- [ ] Add command tests proving partial/pending add exits non-zero after rendering all outcomes.

**Dependencies:** Task 3  
**Files likely touched:** `internal/workspace/workspace.go` or new `internal/workspace/add.go`, workspace tests, `cmd/addrepo.go`, command tests  
**Estimated scope:** Medium (3–4 files)

## Checkpoint: Constructive Operations (after Tasks 3–4)

- [ ] Create/add success and every injected failure satisfy state/filesystem/Git invariants.
- [ ] The original command can resume its pending operation; doctor reports it as a fallback.
- [ ] Recovery records clear after complete success/compensation.
- [ ] `just check`, `just build`, and focused race tests pass.

---

## Phase 3: Destructive Operations

### Task 5: Refuse unsafe repository removal by default

**Description:** Deliver a complete default `remove-repo` path that runs hooks outside the lock, then performs authoritative all-target preflight and clean Git removal without unchecked filesystem fallback.

**Acceptance criteria:**
- [ ] After teardown, immediately revalidate canonical source/path identity, workspace containment, symlink policy, exact Git registration/branch, and dirty state before any removal.
- [ ] If any target fails preflight, mutate none; dirty, unexpected, unregistered, detached/mismatched, or source-missing targets remain intact and in state.
- [ ] Clean expected targets remove successfully with typed per-repo outcomes and no unconditional `RemoveAll`.

**Verification:**
- [ ] Add exact gitops tests for non-forced dirty refusal and porcelain identity parsing.
- [ ] Add exact workspace/command tests for dirty, unexpected path, symlink escape, registration mismatch, branch mismatch, teardown-induced dirty state, and clean success.

**Dependencies:** Tasks 1–3 (independent of Task 4)  
**Files likely touched:** `internal/gitops/gitops.go`, gitops tests, new `internal/workspace/cleanup.go`, cleanup tests, `cmd/removerepo.go`  
**Estimated scope:** Medium (5 files)

### Task 6: Make forced and failed repository removal retryable

**Description:** Extend the safe removal path with `--yes`/bounded `--force`, durable phase progress, aggregated runtime failures, and idempotent retry through both `remove-repo` and doctor visibility.

**Acceptance criteria:**
- [ ] Force may remove a dirty expected target or use filesystem fallback only after canonical identity/boundary checks; it never authorizes an unexpected path.
- [ ] Teardown, worktree removal, forced filesystem removal, branch deletion, or state commit failure remains recorded with repair inputs and a non-zero result.
- [ ] Mixed outcomes preserve failed entries; retry treats already-absent completed stages as success and converges.

**Verification:**
- [ ] Add exact fault tests for every removal phase, mixed two-repo outcomes, state-write failure, retry, and second-call idempotence.
- [ ] Add command tests for confirmation-only `--yes`, destructive `--force`, aggregate rendering, and exit status.

**Dependencies:** Task 5  
**Files likely touched:** `internal/workspace/cleanup.go`, cleanup/workspace tests, `cmd/removerepo.go`, command tests  
**Estimated scope:** Medium (4 files)

### Task 7: Make workspace deletion safe and retryable

**Description:** Route one workspace deletion through the cleanup engine, preserving state/recovery data until repository, workspace-root, branch, and state-removal stages complete; original-command retry must finish interrupted deletion.

**Acceptance criteria:**
- [ ] Global `pre_delete`/repo teardown outcomes are collected outside the lock; service preflight repeats afterward before MCP/path mutation.
- [ ] Per-repo, root-removal, branch, hook, and state-write failures yield cleanup-pending/non-zero without a false success banner, premature state removal, or premature stats event.
- [ ] Retry resumes idempotently and clears workspace/recovery state only when every required stage succeeds.

**Verification:**
- [ ] Add exact tests for dirty/unexpected refusal, hook-induced changes, each runtime phase failure, root-removal failure, commit failure, and retry convergence.
- [ ] Local e2e: default dirty delete preserves data; forced retry completes.

**Dependencies:** Task 6  
**Files likely touched:** `internal/workspace/workspace.go` or new `internal/workspace/delete.go`, deletion tests, `cmd/delete.go`, command tests, `e2e/run.sh`  
**Estimated scope:** Medium (4–5 files)

### Task 8: Aggregate multi-workspace deletion

**Description:** Make interactive multi-select (and, if approved, multiple names) attempt every workspace with one service instance, aggregate hook/service outcomes, and exit only after all targets finish.

**Acceptance criteria:**
- [ ] Failure/abort for one workspace does not prevent later selected workspaces from being attempted.
- [ ] Final output preserves request order and includes each workspace/repository failure with one non-zero exit.
- [ ] Completed workspaces remain completed while pending workspaces retain recovery data.

**Verification:**
- [ ] Add exact command tests for continue-after-hook-failure, continue-after-service-failure, ordering, and aggregate exit.
- [ ] E2E: one dirty and one clean selected workspace leaves the dirty one discoverable and deletes the clean one.

**Dependencies:** Task 7  
**Files likely touched:** `cmd/delete.go`, new `cmd/delete_test.go`, optionally shared human-result rendering  
**Estimated scope:** Small (2–3 files)

## Checkpoint: Destructive Safety (after Tasks 5–8)

- [ ] Default delete/remove cannot erase dirty or unexpected paths.
- [ ] Force is path-bounded and tested.
- [ ] Original commands can resume partial cleanup; doctor reports pending records.
- [ ] Every selected workspace is attempted before aggregate exit.
- [ ] Human reviews changed `--yes`/`--force` behavior.

---

## Phase 4: Reconciliation and Repair

### Task 9: Produce four-way reconciliation plans

**Description:** Upgrade diagnosis from path existence to a read-only plan comparing state entries, canonical filesystem paths, `git worktree list --porcelain`, branches, and all recovery-record kinds.

**Acceptance criteria:**
- [ ] Doctor distinguishes healthy, state-only, path-only, registration-only, branch-mismatched, dirty/unexpected, and operation-pending conditions without deleting evidence.
- [ ] Additive JSON fields provide stable issue/action codes, preconditions, destructive flag, and ordered workspace/repo context while preserving the existing array shape.
- [ ] Diagnosis-only runs have no side effects and return the approved healthy/actionable exit status.

**Verification:**
- [ ] Add exact tests for every four-way mismatch and each create/add/remove/delete/sync/rename record kind.
- [ ] Add compatibility tests that existing `doctor --json | jq 'length'` consumers still work.

**Dependencies:** Tasks 2–8  
**Files likely touched:** `internal/gitops/gitops.go`, gitops tests, new `internal/workspace/reconcile.go`, reconciliation tests, `cmd/doctor.go`  
**Estimated scope:** Medium (5 files)

### Task 10: Repair constructive recovery records idempotently

**Description:** Make `doctor --fix` apply create/add compensation or completion through the same constructive primitives, with precondition checks and repeatable outcomes.

**Acceptance criteria:**
- [ ] Pending create/add can complete or compensate without manual branch/worktree/state edits.
- [ ] Unknown ownership or changed preconditions stop destructive action conservatively and retain the record with a non-zero result.
- [ ] A second fix after convergence performs zero mutations.

**Verification:**
- [ ] Add exact tests for interrupted branch creation, worktree creation, final state commit, failed compensation, stale precondition, and second-fix no-op.
- [ ] E2E: injected pending create/add → doctor diagnosis → fix → healthy second fix.

**Dependencies:** Tasks 3–4, 9  
**Files likely touched:** new `internal/workspace/repair.go`, constructive repair tests, `cmd/doctor.go`, command tests, transactional e2e  
**Estimated scope:** Medium (5 files)

### Task 11: Repair destructive recovery records idempotently

**Description:** Extend `doctor --fix` to resume remove/delete through the same bounded cleanup engine and persist applied/skipped/failed action results.

**Acceptance criteria:**
- [ ] Pending remove/delete completes safely when preconditions still match and never widens force beyond the recorded authorization.
- [ ] Stale/failed repair remains pending with actionable per-repo detail and non-zero exit.
- [ ] Reapplying a completed destructive plan is a no-op.

**Verification:**
- [ ] Add exact tests for each pending cleanup stage, stale paths/registrations, mixed outcomes, repair failure, and second-fix no-op.
- [ ] E2E: partial delete → plan → fix → healthy second fix.

**Dependencies:** Tasks 6–10  
**Files likely touched:** `internal/workspace/repair.go`, destructive repair tests, `cmd/doctor.go`, command tests, transactional e2e  
**Estimated scope:** Medium (5 files)

## Checkpoint: Core Recovery (after Tasks 9–11)

- [ ] Create/add/remove/delete failures are diagnosable and repairable without Git surgery.
- [ ] Doctor never discards the only repair inputs.
- [ ] Fix is idempotent and emits compatible machine-readable issue/action results.

---

## Phase 5: Remaining Mutation Paths

### Task 12: Return structured sync outcomes and repair failed aborts

**Description:** Replace console-only sync failures with ordered per-repository outcomes while retaining parallel execution, and add doctor diagnosis/repair for the only durable sync recovery state: a rebase that could not be aborted.

**Acceptance criteria:**
- [ ] Every repository returns a stable phase/status for fetch, dirty precondition, base resolution, hooks, rebase, and abort; aggregate failure is non-zero after all repos finish.
- [ ] Rebase conflict with successful abort is a normal failed outcome; abort failure writes a sync recovery record visible to doctor.
- [ ] `doctor --fix` retries/validates abort cleanup, clears the record on success, and is idempotent.

**Verification:**
- [ ] Add exact tests for every sync phase, mixed success/failure ordering, conflict+abort success, abort failure record, repair, and second-fix no-op.
- [ ] E2E: one successful and one failing repo both appear before non-zero exit.

**Dependencies:** Tasks 2–3, 9–11  
**Files likely touched:** `internal/workspace/workspace.go` or new `internal/workspace/sync.go`, sync/repair tests, `cmd/sync_cmd.go`, `cmd/doctor.go`, e2e tests  
**Estimated scope:** Medium (5 files)

### Task 13: Harden rename and replace with repair support

**Description:** Bring rename under the same lock/record/compensation contract and extend doctor for incomplete rename. Keep replace explicitly composed: old deletion commits before new creation begins, and no creation follows blocked/pending deletion.

**Acceptance criteria:**
- [ ] Rename failures at state update, filesystem rename, worktree repair, or compensation leave original/new consistency or a doctor-visible repair record.
- [ ] Doctor can conservatively finish/revert rename and a second fix is a no-op.
- [ ] Replace never creates the new workspace unless old deletion fully completes and clearly reports when deletion committed before create failed.

**Verification:**
- [ ] Add exact tests for every rename phase, concurrent rename/create/delete, repair, and second-fix no-op.
- [ ] Add command/e2e tests for blocked dirty replace and delete-committed/create-failed reporting.

**Dependencies:** Tasks 1–3, 7, 9–11  
**Files likely touched:** `internal/workspace/workspace.go` or new `internal/workspace/rename.go`, rename/repair tests, `cmd/create.go`, `cmd/rename.go`, command/e2e tests  
**Estimated scope:** Medium (5 files)

## Checkpoint: Complete Mutation Contract (after Tasks 12–13)

- [ ] Create/add/remove/delete/sync return typed ordered outcomes and meaningful exits.
- [ ] Rename/replace follow documented transaction boundaries.
- [ ] Every recovery-record kind has doctor diagnosis and idempotent repair.
- [ ] No mutating service path bypasses the `Mutation` handle.

---

## Phase 6: Validation and Documentation

### Task 14: Add race and transactional e2e gates

**Description:** Wire the already-working fault tests into required CI and run the full deterministic local e2e lifecycle with a race-instrumented binary. Optional external-network clone checks remain a separately documented ordinary-e2e exclusion.

**Acceptance criteria:**
- [ ] CI runs `go test -race ./...` plus repeated cross-process contention tests.
- [ ] Every deterministic/local e2e section runs with `GW_BIN` built using `-race`, covering dirty refusal, partial failure, recovery, aggregate exits, and convergent repair.
- [ ] Existing optional external-network tests run in ordinary e2e and are the only documented race-e2e exclusion.

**Verification:**
- [ ] `just check && go test -race ./...`
- [ ] `go build -race -o /tmp/gw-race ./cmd/gw && GW_E2E_SKIP_NETWORK=1 GW_BIN=/tmp/gw-race bash e2e/run.sh`
- [ ] `just e2e`

**Dependencies:** Tasks 1–13  
**Files likely touched:** `.github/workflows/ci.yml`, `Justfile`, `e2e/run.sh` and/or new `e2e/transactional.sh`  
**Estimated scope:** Small (3–4 files)

### Task 15: Align lifecycle documentation and release notes

**Description:** Update architecture, workflows, and operations to match the approved/implemented boundaries, then document shipping behavior without claiming issue #63's public machine contract.

**Acceptance criteria:**
- [ ] Docs describe the `Mutation` boundary, source-clone retention, recovery records, hook timing/revalidation, force/exit behavior, and doctor diagnosis/fix in timeless language.
- [ ] Correct stale claims about mutex protection, fsync, state JSON shape, force skipping cleanup, and current doctor capabilities.
- [ ] Release notes identify behavior changes to `--yes`/`--force`, actionable doctor exits, and surfaced partial failures.

**Verification:**
- [ ] Review examples against `gw --help` and deterministic e2e output.
- [ ] `just check && just build && just e2e`

**Dependencies:** Tasks 1–14  
**Files likely touched:** `openwiki/architecture.md`, `openwiki/workflows.md`, `openwiki/operations.md`, `CHANGELOG.md` at release time  
**Estimated scope:** Medium (3–4 files)

## Checkpoint: Complete (after Tasks 14–15)

- [ ] Fault-injection coverage exists for every mutation phase and expected test names are present.
- [ ] Concurrent mutations cannot lose state updates.
- [ ] Interrupted operations are diagnosable and repairable without manual Git surgery.
- [ ] Default deletion preserves dirty/unexpected paths.
- [ ] Automation receives non-zero exit plus complete per-repository details; issue #63 can consume typed results.
- [ ] Unit/full race, build, race-instrumented deterministic e2e, and ordinary full e2e gates pass.
- [ ] Documentation/release notes match runtime behavior.
- [ ] Human approval is recorded before merge/release.

## Fault-Injection Matrix

| Area | Required phases |
|---|---|
| State | lock, load, callback, temp write, file sync, rename, directory sync, holder process death |
| Source acquisition | input validation, clone failure, retained successful clone followed by workspace failure |
| Create | root mkdir, branch create, worktree add/track per repo, state commit, worktree rollback, branch rollback |
| Add | all-input preflight, branch/worktree per repo, state update, rollback |
| Remove | post-hook identity/dirty preflight, worktree remove, forced filesystem remove, branch delete, state update |
| Delete | hook aggregation/revalidation, remove phases, root removal, MCP/stats policy, state removal, next workspace |
| Sync | fetch, status, base, counts, hooks, rebase, abort, abort repair |
| Rename | state transition, filesystem rename, each worktree repair, compensation, doctor repair |
| Doctor | scan each source, plan, precondition recheck, apply, result persistence |

Every case asserts state JSON validity, filesystem paths, `git worktree list --porcelain`, relevant branches, typed result, CLI exit, recovery visibility, and convergence.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Global lock blocks during slow local mutation | Medium | Keep fetch/source acquisition and hooks outside; repeat authoritative checks inside; use a 30-second actionable timeout. |
| Nested mutation deadlocks | High | Explicit non-reentrant `Mutation` handle; no public lock-acquiring calls inside; fail nested/test misuse immediately. |
| Hook changes target after preflight | High | Run hooks outside lock, then perform the complete authoritative preflight immediately before mutation. |
| Recovery deletes a pre-existing branch | High | Persist operation ownership before mutation; repair is conservative when ownership is unknown. |
| `--force` meaning changes | High | Add `--yes`, make implications explicit in help/results/docs, and add compatibility tests. |
| Public JSON work leaks into #59 | Medium | Keep typed results internal/human-rendered; preserve only additive doctor JSON; defer global envelope to #63. |
| Journal grows stale | Medium | Doctor reports stale records; command retry/fix removes completed records; repair is idempotent. |
| Permission-based tests are flaky | Medium | Add instance-scoped seams in Task 2; keep real Git for invariant verification. |
| Setup/teardown shell side effects cannot roll back | Medium | Keep hooks outside core resource atomicity, revalidate after destructive hooks, and surface failures. |

## Human Review Questions

1. Approve the explicit `Mutation` handle with a 30-second lock timeout?
2. Approve `~/.grove/operations/` as the minimal crash-recovery journal?
3. Approve `--yes` for confirmation bypass and `--force` as `--yes` plus bounded destructive authorization?
4. Approve hook behavior: destructive hooks run outside the lock followed by full revalidation; teardown failure blocks that repo; setup/post-create failure returns partial after commit?
5. Approve actionable `gw doctor` findings returning non-zero while preserving the current `doctor --json` array shape?
6. Approve successful remote source clones being retained when later workspace mutation fails?

## Definition of Done

For every task: acceptance criteria pass; named tests exist and fail without the change; existing tests remain green; edge/error paths are covered; formatting/static analysis pass; no unrelated refactor is included; and user-facing behavior is documented. For the epic: every recovery kind is repaired at runtime, integration/race/e2e gates pass, backward compatibility is reviewed, and a human approves both this plan and the final implementation.
