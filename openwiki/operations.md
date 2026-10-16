---
type: Operational runbook
title: Operations and Runbook
description: Configure, diagnose, clean up, extend, and release Grove safely. Covers on-disk state, hooks, Git tracing, plugins, recovery commands, and contributor validation for failed workspace operations.
tags: [grove, operations, runbook, configuration, troubleshooting, maintenance]
sources:
  - id: openwiki-source-3e0d0add2c6f99598546739a
    resource: repo://cmd/bug_report.go
  - id: openwiki-source-4a20513926f0658ee53b3ddf
    resource: repo://cmd/doctor.go
  - id: openwiki-source-a8910515ddd14810ad43f5c1
    resource: repo://internal/config/config.go
  - id: openwiki-source-c7892bbdfc3002a62a48afad
    resource: repo://internal/gitops/gitops.go
  - id: openwiki-source-6e2ef7bffba8b16154bfe6e4
    resource: repo://internal/lifecycle/lifecycle.go
  - id: openwiki-source-a5147957bc4eb2270289c610
    resource: repo://internal/logging/logging.go
  - id: openwiki-source-9fa0f37ad88ce2a52ed9f800
    resource: repo://internal/operations/service.go
  - id: openwiki-source-a31375378633c98afe544d37
    resource: repo://internal/state/lock.go
  - id: openwiki-source-04df3322ec2dc26efbad1e6f
    resource: repo://internal/state/state.go
  - id: openwiki-source-0af407bb538616acaaa4baca
    resource: repo://internal/workspace/doctor.go
generated: { by: "openwiki/0.5.2", at: "2026-10-16T17:04:27.136Z" }
verified:
  - by: openwiki/0.5.2
    at: 2026-10-16T17:04:27.136Z
---

# Operations and Runbook

Grove is a static `gw` binary for macOS and Linux and requires `git` on `PATH`. Grove does not store Git credentials: authentication remains the responsibility of the SSH agent, `~/.ssh`, and Git credential helpers. Git commands run non-interactively (`GIT_TERMINAL_PROMPT=0` and, unless already supplied, `GIT_SSH_COMMAND=ssh -o BatchMode=yes`), so fix credentials or remotes rather than expecting a prompt.

## Install, initialize, and upgrade

Choose an installation path:

```bash
# Homebrew (macOS or Linux)
brew install nicksenap/grove/grove

# Official install script (macOS or Linux)
curl -fsSL https://raw.githubusercontent.com/nicksenap/grove/master/scripts/install.sh | sh

# Go toolchain
go install github.com/nicksenap/grove/cmd/gw@latest
```

The install script verifies the release checksum and chooses `/usr/local/bin`, falling back to `~/.local/bin`; `GW_INSTALL_DIR` and `GW_VERSION` override those choices. A checkout can be built with `go build -o gw ./cmd/gw`, but that is a development path.

Initialize Grove with one or more existing repository roots:

```bash
gw init ~/dev ~/work/microservices
gw repos
```

`gw init` resolves and validates each directory, merges roots without duplicates, creates the Grove and workspace directories, and atomically saves configuration. Commands that need configuration fail with `Grove not initialized. Run: gw init <repo-dir>` when it is absent.

Install shell integration when a command must change the parent shell's directory:

```bash
# Bash or Zsh: add to ~/.bashrc or ~/.zshrc
eval "$(gw shell-init)"

# Nushell
gw shell-init --shell nu | save -f ~/.config/nushell/grove.nu
# Add to config.nu:
source grove.nu
```

The wrapper enables `gw go` to change directory and lets `gw create` auto-cd into a new workspace. `GROVE_CD_FILE` is an internal shell-wrapper variable; do not set it manually except while debugging shell integration.

Upgrade using the method originally chosen:

```bash
brew update && brew upgrade grove
# For the install script, run the curl command again.
go install github.com/nicksenap/grove/cmd/gw@latest
```

Interactive commands may display a cached GitHub release notice. The check is non-blocking, runs at most once per 24 hours, and stores its result in `~/.grove/update-check.json`; `gw unlink-trash` is offline and skips it.

## On-disk contract and configuration

The default Grove directory is `~/.grove`:

| Path | Role | Safe operational treatment |
|---|---|---|
| `~/.grove/config.toml` | Repository roots, workspace root, presets, and global hooks | Edit carefully or use `gw init`; malformed TOML prevents normal loading. |
| `~/.grove/state.json` | Workspace registry: names, branches, timestamps, repo/worktree paths, and optional source metadata | Do not hand-edit during an operation; back it up and use `gw doctor --fix` or workspace commands. |
| `~/.grove/state.lock` | Stable exclusive lock for state-changing operations | Leave it in place; it is separate because `state.json` is replaced atomically. |
| `~/.grove/cache/remotes.json` | Best-effort remote URL cache | Safe to delete; missing or invalid JSON behaves as an empty cache. |
| `~/.grove/update-check.json` | Cached release-notice result | It is independent of workspace state. |
| `~/.grove/plugins/` | Executable `gw-<name>` plugins and `.gw-<name>.json` installer metadata | A copied plugin has no upgrade metadata. |
| `<workspace-dir>/<name>/` | User workspace and Git worktrees; default is `~/.grove/workspaces/<name>/` | Treat as user data, not disposable registry data. |
| `<workspace-dir>/.trash/` | Same-filesystem quarantine during deletion | Use `gw doctor` before removing anything. |
| `~/.grove/grove.log` | Info, warning, and error flight recorder for commands that initialize logging | Inspect with `tail`; it rotates at 1 MiB and keeps three backups. |

A minimal `config.toml` looks like this:

```toml
repo_dirs = ["/home/me/dev", "/home/me/work"]
workspace_dir = "~/.grove/workspaces"

[presets]
backend = { repos = ["svc-auth", "svc-api"] }

[hooks]
post_create = "./scripts/workspace-created {path}"
pre_delete = "./scripts/workspace-closing {path}"
on_close = "./scripts/close-workspace-pane {path}"
```

Preset names must match `[a-zA-Z0-9_-]+`. The loader accepts legacy `repos_dir = "~/dev"` only for migration: it converts it to `repo_dirs = ["~/dev"]` and saves the migrated file. Configuration and state are written through a temporary file followed by rename. A missing state file means an empty workspace list; invalid JSON reports `corrupt state file (...). Run: gw doctor --fix`.

Repository discovery scans configured roots to depth three, does not descend into a repository, resolves remotes in parallel with at most 16 concurrent fetches, and deduplicates the same remote. It prefers a direct child of a configured root over a nested clone. Discovery still runs per command; only remote resolution is cached for 24 hours and invalidated when `.git/config` changes.

A repository-root `.grove.toml` controls worktree behavior:

```toml
base_branch = "stage"
setup = ["pnpm install", "pnpm run build"]
teardown = "rm -rf node_modules"
pre_sync = "pnpm run build:check"
post_sync = "pnpm install"

# Consumed by gw-run, not Grove core:
pre_run = "docker compose pull"
run = "pnpm dev"
post_run = "docker compose down"
```

`setup` accepts a string or list and runs sequentially in a new worktree. `teardown` runs before repo-worktree removal. `pre_sync` and `post_sync` surround sync operations. These per-repository hook failures are warnings and do not block their parent operation. `run`, `pre_run`, and `post_run` are parsed for compatibility and executed by the external `gw-run` plugin.

## Hooks and safety switches

Global hooks are configured under `[hooks]`, run through `sh -c`, and support `{name}`, `{path}`, `{branch}`, `{source_url}`, `{source_ref}`, and `{source_title}`. Values are safely single-quoted, including embedded quotes; unused source placeholders become empty text.

```mermaid
flowchart TD
    create["gw create"] --> resolve["resolve_branch"]
    resolve --> provision["create workspace and state"]
    provision --> post["post_create"]
    add["gw add-repo"] --> added{"new repo added"}
    added -->|yes| addhook["post_add_repo"]
    added -->|no| unchanged["no hook"]
    delete["gw delete or gw prune --yes"] --> pre["pre_delete"]
    pre --> teardown["per-repo teardown"]
    teardown --> remove["quarantine and remove worktrees"]
    close["gw go -c"] --> onclose["on_close"]
```

This shows hook ordering around creation, repo addition, close, and deletion. A post-operation hook failure does not undo the already-completed create or add operation.

A hook may be a command string or metadata table:

```toml
[hooks.post_create]
command = "npm install && npm run build"
description = "Prepare the worktree"
stream = true
timeout = "5m"
on_failure = "abort"
```

Global hooks have distinct operation boundaries:

| Hook | Entry point and ordering | Result or safety note |
|---|---|---|
| `resolve_branch` | `gw create`, before worktree creation | stdout must be exactly one non-empty line after trimming and must pass Git branch validation; failure always aborts creation. Skipped by `--track` and `--no-hooks`. |
| `post_create` | `gw create`, after workspace and state creation | Runs once after creation; an aborting failure reports an error but does not roll back the workspace. |
| `post_add_repo` | `gw add-repo`, after one or more repos are actually added | Does not run when nothing new was added; failure does not undo additions. |
| `pre_delete` | `gw delete`, before destructive worktree removal | An aborting failure prevents deletion. |
| `on_close` | `gw go -c` | Required for the close operation; an absent hook is reported as not configured. |

`resolve_branch` receives `{branch}` as typed, `{name}`, and source placeholders; `{path}` is empty because no workspace exists yet. Its output is never combined with stderr, is trimmed, and must contain no carriage return or newline. `post_create` and `post_add_repo` receive the stored workspace name, path, resolved branch, and any source metadata.

- `stream = false` (default) captures combined output and stays quiet on success; failures echo captured output to stderr with a hook prefix.
- `stream = true` sends line-prefixed progress to stderr, preserving stdout for shell integration.
- `timeout` accepts Go durations such as `30s` or `5m`; on supported macOS/Linux builds Grove kills the hook process group.
- `on_failure = "warn"` (default) logs and continues; `abort` makes the hook failure fatal when the calling operation honors it.

Skip global hooks for one invocation:

```bash
gw --no-hooks create my-feature --repos svc-api
gw -n delete my-feature
```

`--no-hooks` only disables global lifecycle hooks. It does not disable per-repo `setup`, `teardown`, `pre_sync`, or `post_sync`. Hook commands are plain text in `config.toml` and expanded commands are logged, so keep tokens and passwords out of them. A `post_create` failure never rolls back the workspace that was already created: with `on_failure = "warn"` the operation continues, while `on_failure = "abort"` returns an error but leaves the workspace and its state for inspection or later cleanup. Likewise, `post_add_repo` runs only after at least one repo was added; its failure does not undo those additions.

## Git diagnostics and logging

Use the global flag before a subcommand when you need Git traces. The global flag is long-only; it has no `-v` short form:

```bash
gw --verbose create my-feature
gw --verbose sync
tail -n 100 ~/.grove/grove.log
```

`gw status` defines its own local `-V/--verbose` flag for showing full Git status. That local flag shadows the persistent flag, so `gw --verbose status` does **not** enable Git tracing; use `gw status -V` for detailed status output, and use another command such as `gw --verbose sync` when investigating Git calls made by Grove.

For normal commands, the root command's persistent pre-run first performs the interactive-terminal update-notice check, then calls `logging.Setup(verbose)` and records the command at `INFO` level. `INFO`, `WARN`, and `ERROR` records are written regardless of verbose mode once setup has successfully opened `~/.grove/grove.log`; `DEBUG` records are written only when verbose is enabled. Offline commands skip that pre-run, update check, and normal setup; the hidden `gw unlink-trash` command explicitly opens logging in non-verbose mode so its failures can still be recorded. Logging is a file flight recorder, not terminal output: ordinary console messages and errors go to stderr, while query/table and machine-readable output is written to stdout, so diagnostics do not corrupt pipelines.

In verbose mode each Git invocation records the redacted command, working directory, elapsed time, and `ok` or error status. On failure the trace uses Git's combined output rather than only the process exit status; URL userinfo such as `https://user:token@example.com` is replaced with `https://***@example.com` before output is truncated. Failed Git output is capped at 2,048 bytes without splitting a UTF-8 rune, and newlines in the trace status are flattened. Logs rotate after writes push `grove.log` past 1 MiB: the current file becomes `.1`, backups shift through `.3`, and the oldest backup is discarded. Because hook commands, paths, and Git arguments can still reveal sensitive information, do not put secrets in command lines.

For a shareable diagnostic report:

```bash
gw bug-report
# Review the pre-filled issue before submitting.
gw bug-report --print
```

`bug-report` collects version and platform information, Git version, a workspace summary, read-only doctor output, and the last 50 lines of `grove.log`. It opens a pre-filled GitHub issue only in an interactive terminal; with `--print`, piped output, or a failed browser launch it prints instead. Review log content before sharing.

## Plugins and updates

An external plugin is an executable named `gw-<name>`. Unknown commands look in `~/.grove/plugins` before `$PATH`; on Unix Grove replaces its process so the plugin receives the terminal directly. Plugins receive `GROVE_DIR`, `GROVE_CONFIG`, `GROVE_STATE`, and, when the current directory is registered inside a workspace, `GROVE_WORKSPACE`. Plugins should use Grove commands rather than mutating state files directly.

The embedded registry currently contains `code` and `dispatch`:

```bash
gw plugin search
gw plugin install code
gw plugin install dispatch
gw plugin install nicksenap/gw-run
gw plugin install nicksenap/gw-recipe
```

Install selects the current OS/architecture release asset, requires HTTPS, verifies a release checksum when available, and stores the executable beside `.gw-<name>.json` metadata. Manage installed plugins with:

```bash
gw plugin list
gw plugin list --json
gw plugin upgrade dispatch
gw plugin upgrade                 # installer-managed plugins only
gw plugin remove dispatch

# Manual installation (not upgrade-managed)
cp my-plugin ~/.grove/plugins/gw-myplugin
chmod +x ~/.grove/plugins/gw-myplugin
```

`upgrade` uses saved repository metadata; manually copied plugins are skipped because they have none. A bare install name must be in the embedded registry; an `owner/repo` reference bypasses that lookup.

## Safe deletion and prune cleanup

Workspace deletion is a two-phase operation:

```mermaid
sequenceDiagram
    participant User
    participant Grove
    participant Git
    participant Trash
    User->>Grove: gw delete NAME
    Grove->>Grove: preflight worktrees
    Grove->>Grove: run per-repo teardown
    Grove->>Grove: reload and lock state
    alt workspace path exists
        Grove->>Trash: rename workspace to .trash/NAME-timestamp
    else workspace path is missing
        Grove->>Grove: skip quarantine
    end
    Grove->>Git: prune worktree registrations
    Grove->>Grove: remove state entry
    Grove->>Grove: delete non-preserved branch
    Grove-->>User: report deletion
    opt quarantined bytes remain
        Grove->>Trash: start detached unlink-trash
    end
```

This sequence explains why deletion can return while large workspace bytes are still being unlinked. Teardown runs before the mutation lock because it is user-owned and may invoke `gw`; Grove reloads and rechecks state and worktrees under the lock before mutating. A normal delete rejects dirty, unexpected, or unregistered worktrees unless forced. If pruning or state removal fails, Grove attempts to restore the quarantined root and repair worktree references. A missing-directory record has no root to restore but still gets Git registrations, state, and branches cleaned up. Branches marked for preservation are not deleted; branch-delete failures are warnings after state cleanup.

Quarantine unlinking is best effort. A detached unlink process is used when possible; a spawn failure falls back to synchronous removal, and an unlink failure is logged as a warning. The hidden offline command accepts only a direct child of `.trash`, preventing arbitrary recursive deletion:

```bash
gw unlink-trash PATH   # internal; do not use for arbitrary paths
```

`gw prune` previews records created at least seven days ago by default, plus records whose workspace directory is missing regardless of age. `--min-age` accepts `12h`, `7d`, or `2w`; a bare number means days, and negative or invalid values are rejected. Malformed creation timestamps are not selected unless the directory is missing. Missing candidates are marked `directory missing` in the table and `missing: true` in JSON. Preview is read-only.

```bash
gw prune                         # preview old or missing records
gw prune --min-age 12h
gw prune --min-age 2w
gw prune --min-age 14             # 14 days
gw prune --json
gw prune --yes --min-age 7d       # attempt every candidate
```

With `--yes`, every candidate is attempted through the same forced deletion path as `gw delete`; one failure does not stop later candidates. Human output reports `FAILED: ...`, JSON adds `error`, and the command exits non-zero if any deletion failed.

## Doctor and recovery

Run the health check before hand-editing configuration or state:

```bash
gw doctor
gw doctor --json
gw doctor --fix
```

Without `--fix`, doctor is read-only and reports missing workspace roots, missing source repositories, missing worktree directories, and worktree/path problems, as well as leftover quarantine entries. With `--fix`, state repairs run under `state.lock`: missing workspace records and stale repo entries are removed when safe. Trash cleanup happens outside the mutation lock. Doctor refuses to remove bytes whose name may still belong to a registered workspace and instead reports `restore quarantined workspace or retry delete`. Other leftover trash is removed by `--fix`. A healthy human-readable run prints `All workspaces healthy`; JSON is suitable for automation.

Use these recovery paths:

- **Not initialized or no repositories:** run `gw init <existing-directory>`, then `gw repos`.
- **Malformed configuration:** correct TOML and retry. Loading a legacy `repos_dir` performs the supported migration; do not maintain both fields.
- **Corrupt state:** stop concurrent Grove commands, copy `~/.grove/state.json` for safekeeping, and run `gw doctor --fix`. Restore a known-good backup if needed; do not delete workspace directories just to repair JSON.
- **Missing workspace or stale registry:** run `gw doctor` first. If the directory is truly gone, `gw doctor --fix` removes the record; if matching bytes remain in `.trash`, restore them or retry deletion instead of sweeping them.
- **Leftover `.trash`:** confirm no delete or doctor cleanup is active, then use `gw doctor` and `gw doctor --fix`. Never pass an arbitrary path to `unlink-trash`.
- **Git authentication:** load the intended key (`ssh-add ~/.ssh/id_ed25519`), test `ssh -T git@github.com`, and correct the remote or credential helper. Grove will not prompt interactively.
- **Git clone, fetch, rebase, or worktree failure:** rerun with `gw --verbose ...`, inspect the redacted Git trace and the command's error, then verify the source repository, remote, branch, and credentials. Resolve conflicts or dirty files in the worktree before retrying; do not delete the workspace as a first response.
- **Hook failure or timeout:** inspect prefixed stderr and `grove.log`; enable `stream = true`, increase `timeout`, or select `on_failure = "warn"` only when continuing is safe. A failed `post_create` does not roll back the created workspace, so use `gw doctor`, inspect the workspace, and delete it deliberately if it is unusable. A failed `post_add_repo` likewise leaves newly added repos in place. Per-repo setup has no Grove timeout, so supervise unusually long commands in the command itself.
- **Workspace path already exists:** use `gw doctor` to distinguish live data, stale state, and quarantine data. Do not overwrite it manually; delete the registered workspace or choose another name after checking for uncommitted work.

## Contributor validation and releases

These checks are for source development and operational validation, not routine end-user troubleshooting:

```bash
just build
go build -o gw ./cmd/gw
just check                 # the CI gate
go test ./internal/workspace -v
go test ./internal/discover -v
go test ./internal/config -v
just e2e
GW_BIN="$PWD/gw" go test ./e2e -run TestCreateWorkspace -count=1 -v
```

CI invokes exactly `just check`; it does not run separate checks. That recipe runs unit tests, e2e, vet, formatting validation, gocyclo, staticcheck, and the dead-code gate. The dead-code gate fails if `go tool deadcode ./...` prints unreachable functions; test-only helpers belong in `_test.go`. `just e2e` builds `gw` first and uses isolated fixtures; the public HTTPS clone scenario is opt-in with `GROVE_EXTERNAL_E2E=1`.

When changing persistence or cleanup, focused tests are especially valuable: configuration tests cover defaults and legacy migration; state tests cover atomic persistence and cross-process locking; discovery tests cover depth-three scanning, cache invalidation, parallel resolution, and deduplication; prune tests cover duration units, missing-directory candidates, deletion failures, and JSON; doctor and removal tests cover trash ownership, restoration, branch deletion, missing-directory deletion, and guarded unlinking.

Prepare a release by updating `CHANGELOG.md`, then tag and push it:

```bash
just release X.Y.Z
```

The recipe creates and pushes annotated tag `vX.Y.Z`. The release workflow reruns `just check`, extracts release notes, and invokes GoReleaser. GoReleaser publishes static `darwin` and `linux` `amd64` and `arm64` archives, checksums, and the Homebrew formula. Generated artifacts belong to release automation, not an end-user workstation.

## Related guidance

- [Quickstart](quickstart.md) for first-use commands.
- [Workflows](workflows.md) for create, sync, reset, and delete behavior.
- [Integrations](integrations.md) for terminal and plugin integration.
- [Testing](testing.md) for the broader contributor test strategy.
