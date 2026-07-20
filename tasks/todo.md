# Issue #59 Task List

Source plan: [`tasks/plan.md`](plan.md)  
Target: [Make workspace operations transactional and recoverable](https://github.com/nicksenap/grove/issues/59)

## Human Review Gate

- [x] Approve explicit `Mutation` handle and 30-second lock timeout.
- [x] Approve minimal `~/.grove/operations/` recovery journal.
- [x] Approve `--yes` and bounded destructive `--force` semantics.
- [x] Approve hook timing, revalidation, and failure policy.
- [x] Approve actionable `gw doctor` findings returning non-zero.
- [x] Approve retention of successfully cloned source repos after later workspace failure.

Approved by the human reviewer. Update the plan before implementation if any decision changes.

## Phase 1: State and Recovery Foundations

- [x] **Task 1:** Serialize state mutations across processes. *(Dependencies: none)*
- [x] **Task 2:** Persist and surface recovery records. *(Dependencies: Task 1)*

### Checkpoint: State Foundation

- [x] Concurrent subprocess updates are lossless.
- [x] Recovery records and fault seams survive interruption.
- [x] Doctor can identify a stranded record.
- [x] Existing state JSON remains compatible.
- [x] Human approves the lifecycle contract.

## Phase 2: Constructive Operations

- [x] **Task 3:** Make workspace creation transactional and recoverable. *(Dependencies: Tasks 1–2)*
- [ ] **Task 4:** Make multi-repository additions atomic and recoverable. *(Dependencies: Task 3)*

### Checkpoint: Constructive Operations

- [ ] Create/add failures compensate fully or remain recoverable.
- [ ] Original commands can safely resume pending operations.
- [ ] No operation-created branch/worktree leaks after successful compensation.
- [ ] Focused race tests, `just check`, and `just build` pass.

## Phase 3: Destructive Operations

- [ ] **Task 5:** Refuse unsafe repository removal by default. *(Dependencies: Tasks 1–3)*
- [ ] **Task 6:** Make forced and failed repository removal retryable. *(Dependencies: Task 5)*
- [ ] **Task 7:** Make workspace deletion safe and retryable. *(Dependencies: Task 6)*
- [ ] **Task 8:** Aggregate multi-workspace deletion. *(Dependencies: Task 7)*

### Checkpoint: Destructive Safety

- [ ] Default cleanup preserves dirty and unexpected paths.
- [ ] Force remains bounded by canonical path/source checks.
- [ ] Original commands can resume partial cleanup; doctor reports it.
- [ ] Every selected workspace is attempted before aggregate exit.
- [ ] Human reviews CLI behavior changes.

## Phase 4: Reconciliation and Repair

- [ ] **Task 9:** Produce four-way reconciliation plans. *(Dependencies: Tasks 2–8)*
- [ ] **Task 10:** Repair constructive recovery records idempotently. *(Dependencies: Tasks 3–4, 9)*
- [ ] **Task 11:** Repair destructive recovery records idempotently. *(Dependencies: Tasks 6–10)*

### Checkpoint: Core Recovery

- [ ] Create/add/remove/delete failures are diagnosable and repairable.
- [ ] Doctor preserves repair inputs and compatibility.
- [ ] Reapplying completed repair is a no-op.
- [ ] Recovery succeeds without manual Git surgery.

## Phase 5: Remaining Mutation Paths

- [ ] **Task 12:** Return structured sync outcomes and repair failed aborts. *(Dependencies: Tasks 2–3, 9–11)*
- [ ] **Task 13:** Harden rename and replace with repair support. *(Dependencies: Tasks 1–3, 7, 9–11)*

### Checkpoint: Complete Mutation Contract

- [ ] Create/add/remove/delete/sync return ordered typed outcomes and meaningful exits.
- [ ] Rename/replace follow documented transaction boundaries.
- [ ] Every recovery-record kind has doctor diagnosis and repair.
- [ ] No mutating service path bypasses the `Mutation` handle.

## Phase 6: Validation and Documentation

- [ ] **Task 14:** Add race and transactional e2e gates. *(Dependencies: Tasks 1–13)*
- [ ] **Task 15:** Align lifecycle documentation and release notes. *(Dependencies: Tasks 1–14)*

### Checkpoint: Complete

- [ ] Named fault-injection tests cover every mutation phase.
- [ ] `just check` passes.
- [ ] `go test -race ./...` passes.
- [ ] `just build` passes.
- [ ] Full deterministic e2e passes with a race binary.
- [ ] Ordinary full `just e2e` passes.
- [ ] OpenWiki and release notes match runtime behavior.
- [ ] Human reviews and approves the completed implementation.
