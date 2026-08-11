# Prepared workspace claiming spike

Issue #77 tested whether an anonymous, prepared set of detached Git worktrees can safely become a normal named Grove workspace without rebuilding its dependencies.

## Result: go with a stable backing path

Keep each prepared slot at an immutable physical path and expose it at claim time through a named symlink in `workspace_dir`. Do not relocate a prepared slot.

The local threat model serializes cooperating Grove processes and detects accidental or adversarial path/identity changes before mutation. An Oven-owned `0700` root protects against other local users. A process with the same user identity can still race filesystem operations; immediate identity revalidation narrows that window, but Grove cannot sandbox or fully exclude its own OS user and must quarantine any observed mismatch.

| Strategy | Git after claim | Bun-style dependencies | Python virtual environments | Result |
|---|---|---|---|---|
| Rename the prepared root, then `git worktree repair` | Works | Relative `.bin` links worked | Absolute virtualenv launchers broke | No-go |
| Keep the backing root stable; add a named symlink | Works through the alias | Worked without reinstalling | Worked without recreating the environment | Go |

The committed integration spike uses POSIX-portable Bun-style and Python-style dependency probes (`/bin/sh`, executable modes, and symlinks). A separate ignored local test prepared a path-sensitive multi-language monorepo with its normal Bun and Python package managers, attached a branch through a named alias, and successfully ran both prepared toolchains without reinstalling. No private path, repository name, command, or credential is committed.

## Claim contract for the local Oven

A production claim in #78 should perform this sequence while holding Grove's existing cross-process state lock:

1. Load the slot from a trusted Oven inventory keyed by generation and an unpredictable slot ID. A file inside the slot cannot authenticate itself.
2. Persist a slot-specific claim intent and nonce, transitioning `ready → claiming`. This bounded lifecycle record is not a generic transaction journal.
3. Verify the inventory identity, immutable backing path, unique safe repository names, allowlisted source repositories, exact child-path containment, exact commits, detached worktree registrations, clean tracked files, target name, target path, and branch availability for every repository.
4. Revalidate each path and Git identity immediately before mutating it, then attach the requested branch at its recorded commit.
5. Atomically publish the named workspace alias with symlink creation. An existing path must fail rather than be replaced.
6. Revalidate the alias, backing root, worktrees, branches, and commits, then add the complete ordinary `models.Workspace` entry to `state.json` last. Persist repository paths through the named alias, and retain an external claimed-slot record containing the exact workspace name, alias, branch, and repository set.
7. Transition the external slot record to `claimed`, preserving the backing identity required by delete and diagnostics.
8. Run lifecycle hooks only after releasing the mutation lock.

Prepared slots remain absent from normal workspace state until step 6. `state.json`, not directory existence, is the readiness authority; a pre-state alias is an incomplete claim. Claim and discard must use the same lock and ownership preflight, so only one can win.

## Failure and recovery behavior

The test-only claim harness injects failures from each branch-assignment operation, alias publication, the final state write, and an uncertain state write that committed before returning an error. Recoverable in-process failures:

- leave no successful workspace state;
- remove only an alias still pointing to the owned backing root;
- detach repositories back at their exact prepared commits;
- compare-and-delete only branches created by that claim;
- preserve prepared dependencies for retry or discard.

If state removal is uncertain, rollback preserves the state, alias, worktrees, and branches and quarantines the claim rather than creating dangling state. All repository identities are preflighted before the first mutation and again at use. A concurrent claim or discard from a separate state-store instance waits for the lock, then fails closed after the winner changes the slot identity.

Discard also has a partial-removal window because worktrees are removed sequentially. An ordinary error or hard termination may leave only part of a slot. #78 reconciliation must recognize that state and idempotently remove only worktrees that still match the trusted inventory.

A hard process termination between filesystem/Git mutation and the final state write cannot be made atomic with `state.json`. Do not add a generic transaction framework. The per-slot `claiming` intent must identify the target name, branch, generation, nonce, exact refs, and resources created by the claim. A fixed recovery matrix should either finish a fully matching claim, compare-and-delete its owned refs and alias, or quarantine on any mismatch; it must never infer ownership from names or expose the slot as ready.

## Normal workspace behavior proved

After a stable-path claim, the spike verifies:

- the named workspace is a normal state entry;
- Git status, branch operations, commits, and worktree registration work through the alias;
- prepared Bun-style and Python-style commands run through the alias;
- direct `Service.Status` works through a healthy alias;
- ownership-aware normal deletion removes the registered worktrees and named alias safely;
- dangling, retargeted, and replaced-worktree aliases make deletion fail closed while preserving both owned and replacement paths.

A claimed slot's immutable backing identity and exact expected `models.Workspace` identity must remain in an external Oven ownership record consulted by destructive preflight and diagnostics; persisting only the alias path weakens Grove's ownership guarantees. Destructive preflight must reject extra, missing, duplicate, or changed repository entries before passing state to ordinary deletion. Delete revalidates backing, source, worktree registration, and branch identity but permits normal user commits after claim. The Oven owns the now-empty physical slot root and its metadata after verified normal workspace deletion. #78 must remove or recycle that backing root; Grove's ordinary workspace model should not gain generic pool metadata.

## Required #78 hardening

Before exposing a user command, the local Oven implementation must add:

- a trusted Oven inventory with unpredictable slot identities and lifecycle records outside the aliased tree;
- unique safe repository names, allowlisted source identities, and strict canonical child-path containment;
- immutable generation and backing paths on one local filesystem, in an Oven-owned directory that is not writable by untrusted local users and has no mutable symlink components;
- locked, idempotent claim and discard operations with identity revalidation immediately before each mutation;
- fail-closed reconciliation for interrupted and partially discarded slots using explicit lifecycle transitions and compare-and-delete refs;
- ownership-aware delete/status diagnostics that verify the alias still targets the recorded backing root;
- diagnostics for missing aliases, missing backing roots, replaced paths, and mismatched worktree registrations;
- backing-root cleanup only after verified normal workspace deletion;
- tests for alias/path tampering, uncertain state writes, state-removal failures, and partial discard;
- the existing rule that secrets, credential files, and command logs do not enter ready slots.

The spike intentionally adds no public command, pool, scheduler, production state schema, or generic cache.
