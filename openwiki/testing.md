---
type: engineering validation guide
title: Testing and Change Validation
description: A practical guide to validating Grove changes from focused Go tests through repository-wide checks and compiled-binary end-to-end scenarios. It maps high-value invariants, test isolation patterns, optional network coverage, and release validation to the commands that exercise them.
tags: [testing, validation, quality, release]
verified:
  - by: openwiki/0.5.0
    at: 2026-09-18T19:54:31.983Z
sources:
  - id: openwiki-source-164e2da859b5277df81c7d94
    resource: repo://.github/workflows/ci.yml
  - id: openwiki-source-4d1d392666be6dfdd7a91a2e
    resource: repo://.github/workflows/release.yml
  - id: openwiki-source-7c7c6718eb79bf9eb7df2328
    resource: repo://e2e/doctor_test.go
  - id: openwiki-source-640eefd40cff46943d0dff10
    resource: repo://e2e/environment_test.go
  - id: openwiki-source-6b1859c6bc35c4f29682760b
    resource: repo://e2e/lifecycle_test.go
  - id: openwiki-source-f190971660c28b50aa32c21f
    resource: repo://e2e/main_test.go
  - id: openwiki-source-b442d335606a7325d9a4c71e
    resource: repo://e2e/plugin_test.go
  - id: openwiki-source-758c5518e845e43d580022be
    resource: repo://e2e/repositories_test.go
  - id: openwiki-source-731a81e0c2a64723d89c6642
    resource: repo://internal/lifecycle/lifecycle_test.go
  - id: openwiki-source-159c0388f01321b7027be0b3
    resource: repo://internal/plugin/install_test.go
  - id: openwiki-source-f7cb543e00e02855f6dbc0fe
    resource: repo://internal/plugin/plugin_test.go
  - id: openwiki-source-421db17455c6dcaa66a012f6
    resource: repo://internal/state/state_test.go
  - id: openwiki-source-aa274c4791897b80a3231f91
    resource: repo://internal/workspace/create_test.go
  - id: openwiki-source-0aec4bdc0372145df6edc477
    resource: repo://internal/workspace/doctor_test.go
  - id: openwiki-source-0cf556597f692ad56afe2a7d
    resource: repo://internal/workspace/remove_test.go
  - id: openwiki-source-cdc0966b8ab854c5a6d78e92
    resource: repo://internal/workspace/repos_test.go
  - id: openwiki-source-88511b2d7d6e9cfdecfbcdd0
    resource: repo://internal/workspace/reset_test.go
  - id: openwiki-source-7d98bfa7edf9d3027b817e8f
    resource: repo://internal/workspace/sync_test.go
  - id: openwiki-source-14e6a4e1ddc06973b783ff20
    resource: repo://Justfile
generated: { by: "openwiki/0.5.0", at: "2026-09-18T19:54:31.983Z" }
---

# Testing and Change Validation

Grove has three useful validation layers: focused package tests for a change, the repository quality gate, and end-to-end tests that invoke the compiled `gw` binary. Start narrow and preserve the full failure output; broaden only after the smallest relevant test is green.

```mermaid
flowchart TD
    changed["Identify changed behavior"] --> focused["Run focused test or package"]
    focused --> pass1{"Pass"}
    pass1 -->|"no"| fix["Inspect failure and revise"]
    fix --> focused
    pass1 -->|"yes"| packages["Run just test or just check"]
    packages --> e2e["Build and run just e2e"]
    e2e --> release["For release run go test ./... and release checks"]
```

This flow shows the recommended progression from a targeted proof to repository and binary-level validation.

## Fastest useful commands

Use the smallest command that proves the behavior under change:

```sh
# One test, with diagnostics
go test ./internal/workspace -run TestCreateRollbackOnFailure -v

# One package
go test ./internal/lifecycle -run 'TestRun|TestExpand' -v
go test ./internal/state -run 'TestWithLock|TestAtomicWrite' -v

# All Go package tests
just test
# Equivalent: go test ./... [optional extra arguments]

# Full local quality gate
just check

# Compiled-binary end-to-end suite
just e2e
```

`just check` runs `test`, `vet`, `fmt-check`, `gocyclo`, and `staticcheck`. `fmt-check` fails if `gofmt -l .` reports files; it does not rewrite them. The complexity and static-analysis recipes install `gocyclo` or `staticcheck` when absent. Use `gofmt -w .` deliberately to repair formatting rather than treating the check as a formatter.

For a single command or CLI parsing change, the `cmd` tests are the narrowest entrypoint. The command tests cover argument derivation and validation, prune preview JSON and age validation, plugin argument handling, and propagation/waiting of delete failures. Package tests under `internal/` are preferable when the behavior belongs to a service rather than Cobra wiring.

## What the package tests protect

### Workspace lifecycle and repository mutations

`internal/workspace` tests are the main safety net for worktree operations:

- `create_test.go` verifies successful multi-repository creation, duplicate workspace names and branches, automatic branches, track mode and fallback, source metadata persistence, setup hooks, progress output, and concurrent creates. Its rollback cases require failed creation to remove worktrees, state entries, and branches created by that invocation while preserving a pre-existing workspace root.
- `repos_test.go` covers adding and removing repositories, idempotent additions, branch conflicts, setup and teardown hooks, and progress reporting. The rollback tests require earlier additions to be undone, their newly-created branches to be removed, and branches that existed before the operation to survive. A failed worktree add must also leave a pre-existing blocked path untouched.
- Dirty worktrees are protected explicitly: remove rejects dirty trees by default, while force removal is tested separately. Reset and sync tests ensure dirty “wanderers” are skipped unless discard is requested, and a rebase conflict aborts rather than silently completing.
- `remove_test.go` covers whole-workspace deletion, dirty and unmerged branch policy, preflight of every repository before mutating any, partial-failure state preservation, repair failures, asynchronous trash unlinking, and immediate recreation of a deleted name. `remove_options_test.go` protects the precondition that prevents deleting a workspace that was recreated between resolution and execution.
- `rename_test.go` checks duplicate-name rejection and preservation of `CreatedAt`; status tests cover current and detached branches, JSON, verbose output, and multi-repository reporting. Prune unit tests cover the age boundary, invalid timestamps, time-zone parsing, and input order.

These tests express the important invariant: a multi-repo operation is not successful merely because one worktree changed. State, branches, directories, and untouched user data must agree after success or rollback.

### State persistence and concurrency

`internal/state/state_test.go` verifies empty and missing stores, CRUD and path lookup, JSON persistence, and atomic writes (including cleanup of the temporary file). Its lock tests use helper processes to prove that processes serialize on the lock, then run twelve concurrent read-modify-write workers to ensure no state entries are lost. Run these tests after changes to workspace persistence, locking, or any operation that updates more than one state record.

### Lifecycle hooks

`internal/lifecycle/lifecycle_test.go` protects both safety and observable failure behavior:

- placeholder expansion includes workspace, path, branch, and source values;
- shell quoting keeps spaces, quotes, newlines, semicolons, pipes, command substitutions, and backticks as data rather than executable syntax;
- disabled hooks return the no-hook result without running, while enabled hooks run;
- `stream = true` prefixes live output with the hook name, successful non-streaming hooks stay quiet, and failed non-streaming hooks echo captured output;
- `on_failure = "abort"` marks the error abortable;
- timeouts report promptly and kill child processes, not only the shell; an invalid timeout is warned about and ignored rather than preventing the hook from running.

The corresponding e2e tests (`e2e/lifecycle_test.go`) verify the user-visible lifecycle: post-create and pre-delete hooks fire, `--no-hooks` and `-n` skip them, source placeholders reach commands, streaming is prefixed, and abort versus warning behavior changes create success as configured.

### Discovery, plugins, and diagnostics

Repository discovery is exercised end to end by `TestRecursiveRepositoryDiscovery`: nested Git repositories are found by `add-dir`, then usable by workspace creation. The repository/completion tests also ensure already-attached repositories are excluded from add suggestions and outside-workspace repositories do not leak into workspace-specific completion. Keep these tests when changing discovery or deduplication logic; duplicate discovery can otherwise produce incorrect repository counts or repeated worktrees.

Plugin tests cover the dispatch boundary as well as management. `internal/plugin` checks plugin-directory lookup, executable filtering, safe-name rejection, metadata cleanup, release selection, conventional public-release fetching versus API fallback, platform asset matching, and archive checksum verification (including bad or missing checksums). `e2e/plugin_test.go` installs a temporary executable under `~/.grove/plugins`, verifies `plugin list` and JSON output, invokes an unknown top-level command through the `gw-<name>` fallback with arguments and `GROVE_DIR`, and verifies removal and unknown-command failures.

Doctor and quarantine behavior is covered by `internal/workspace/doctor_test.go` and the e2e doctor tests. Doctor detects missing worktrees, missing workspace directories, and leftover `.trash` entries; normal mode reports without deleting, while `--fix` removes repairable stale entries. Deletion moves paths through trash and unlinks asynchronously, so tests and callers must allow the background unlink to finish rather than assuming the path disappears synchronously.

## End-to-end environment and scope

The `e2e` package tests Grove as a user would, not by calling internal services. `TestMain` uses `GW_BIN` when supplied; otherwise it builds `./cmd/gw` into a temporary executable. It rejects a missing, directory, or non-executable binary before running tests. Each test gets an isolated temporary `HOME`, repositories, `.grove` directory, and Git identity; system Git configuration and ambient Git variables are constrained. Individual scenarios can therefore be run with `go test ./e2e -run TestName`.

The required e2e suite uses local repositories and `file://` remotes, including workspace lifecycle, repository add/remove, tracking an existing remote branch, source metadata, sync/reset, presets, diagnostics, plugins, prune, and doctor. The public HTTPS clone test is deliberately opt-in:

```sh
GROVE_EXTERNAL_E2E=1 GW_BIN="$PWD/gw" go test ./e2e -count=1
```

`GROVE_EXTERNAL_E2E=1` enables only the public HTTPS clone check; it is not needed for the deterministic suite. `GW_BIN` points tests at an already-built binary. If it is unset, e2e builds one itself. `just e2e` builds `gw` first and runs `GW_BIN="<repository>/gw" go test ./e2e -count=1`; it does not set `GROVE_EXTERNAL_E2E`.

The e2e helpers use a 45-second per-command default timeout and retain stdout, stderr, exit code, and errors in assertions. Failed commands are reported with the complete invocation and both output streams, which makes e2e failures actionable rather than reducing them to “non-zero exit.”

## CI and release checks

Pull requests and pushes to `master` run two CI paths. The `check` job runs `go test ./cmd/... ./internal/...` with `GIT_CEILING_DIRECTORIES=/`. After it passes, the `e2e` job builds `gw` and runs `go test ./e2e -count=1` with `GW_BIN` set to the built binary and the same Git ceiling. A separate install-script matrix runs the installer on Ubuntu and macOS and checks the resulting `gw --version`.

A tagged release is validated by `.github/workflows/release.yml` with `go test ./...` before release notes extraction and GoReleaser. Locally, use `just check` plus `just e2e`; for a release candidate also run `go test ./...` to match the release workflow exactly. The `release` Just recipe creates and pushes an annotated `v<version>` tag, which triggers that workflow, so do not use it as a substitute for a clean validation run.

## Validation checklist

1. Identify the owning package and run the narrowest named test (`go test ... -run ... -v`).
2. If state, rollback, concurrency, hooks, discovery, plugins, dirty worktrees, or doctor behavior changed, run the focused invariant tests listed above rather than only a happy-path test.
3. Run `just test`, then `just check` before broad integration testing.
4. Run `just e2e`; use an explicit `GW_BIN` when testing a particular build and add `GROVE_EXTERNAL_E2E=1` only when network-dependent HTTPS coverage is intentional.
5. For release work, confirm `go test ./...`, installer behavior, and changelog/tag readiness before invoking `just release`.
