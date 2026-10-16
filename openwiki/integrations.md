---
type: concept
title: Integrations
description: Public integration contracts between Grove and Git, shells, lifecycle hooks, external plugins, source providers, editors, installers, and machine-oriented consumers.
tags: [grove, integrations, git, shells, hooks, plugins]
verified:
  - by: openwiki/0.5.2
    at: 2026-10-16T17:04:27.136Z
sources:
  - id: openwiki-source-5fb548baa501bcdc358d79a6
    resource: repo://cmd/create.go
  - id: openwiki-source-b33438ed586700495e3d7655
    resource: repo://cmd/query_output.go
  - id: openwiki-source-d88dd56dbc4620bda4c35f1c
    resource: repo://cmd/root.go
  - id: openwiki-source-fdc127993fc61e535879a71e
    resource: repo://cmd/shellinit.go
  - id: openwiki-source-d191e32409095eff0351d594
    resource: repo://docs/hooks.md
  - id: openwiki-source-6b1859c6bc35c4f29682760b
    resource: repo://e2e/lifecycle_test.go
  - id: openwiki-source-c7892bbdfc3002a62a48afad
    resource: repo://internal/gitops/gitops.go
  - id: openwiki-source-6e2ef7bffba8b16154bfe6e4
    resource: repo://internal/lifecycle/lifecycle.go
  - id: openwiki-source-a5147957bc4eb2270289c610
    resource: repo://internal/logging/logging.go
  - id: openwiki-source-1d1ad5865ebad91142ed8aee
    resource: repo://internal/models/models.go
  - id: openwiki-source-9fa0f37ad88ce2a52ed9f800
    resource: repo://internal/operations/service.go
  - id: openwiki-source-54ed4ed40a03cd339a0bec80
    resource: repo://internal/output/output.go
  - id: openwiki-source-ab647e2cb1e7b3b70e7883be
    resource: repo://internal/plugin/plugin.go
  - id: openwiki-source-315bbc2fab6a49cc711be6d2
    resource: repo://internal/workspace/create.go
generated: { by: "openwiki/0.5.2", at: "2026-10-16T17:04:27.136Z" }
---

# Integrations

Grove is a process- and file-oriented coordinator. It owns workspace records, Git worktrees, and the files under its Grove directory; shells, hooks, plugins, source providers, and automation cross that boundary through commands, environment variables, stdout/stderr, and JSON state. An integration should use Grove commands and the documented environment rather than editing Grove-owned state directly.

## External plugins

A plugin is an executable named `gw-<name>`. Cobra resolves built-in commands first. Only an unknown command enters plugin fallback, so an executable cannot override a built-in command with the same name. Grove looks in this order:

1. `~/.grove/plugins/gw-<name>`
2. `gw-<name>` on `$PATH`

The plugin receives the arguments after its name and inherits the invoking environment. On Unix Grove replaces itself with the plugin; on Windows it starts a child process, forwards standard streams, and propagates the child's exit code. Names must start with an ASCII letter or digit and may then contain letters, digits, `-`, `_`, or `.`.

```mermaid
sequenceDiagram
    participant User
    participant Grove as gw
    participant Resolver as Plugin resolver
    participant Plugin as gw plugin
    User->>Grove: gw name args
    Grove->>Grove: Resolve built-in commands
    alt built-in command
        Grove-->>User: Run built-in
    else unknown command
        Grove->>Resolver: Find gw-name
        Resolver->>Resolver: Check Grove plugin directory then PATH
        Resolver-->>Grove: Executable path
        Grove->>Plugin: Execute with forwarded args and environment
        Plugin-->>User: Standard output and exit status
    end
```

*This diagram shows dispatch only. Plugins are not an API for replacing or directly mutating Grove's state.*

Grove adds these variables to the plugin environment while preserving the rest of the environment:

| Variable | Meaning |
|---|---|
| `GROVE_DIR` | Grove configuration directory, normally `~/.grove` |
| `GROVE_CONFIG` | Path to `config.toml` |
| `GROVE_STATE` | Path to `state.json` |
| `GROVE_WORKSPACE` | Current workspace name when the working directory is inside one; otherwise omitted |

Plugins can inspect those paths and invoke `gw` commands. Core does not prescribe a plugin storage format, editor, agent, notification service, or provider integration. For example, `gw-run` consumes additional `.grove.toml` keys, `gw-code` opens an editor workspace, and `gw-dispatch` starts an agent command; these behaviors remain outside Grove core.

`gw plugin list` (or `--json`) reports executable plugins in the user plugin directory, not arbitrary `$PATH` executables. `gw plugin remove <name>` removes only the user-directory executable and adjacent installer metadata. The embedded registry used by `gw plugin search` and bare-name installation is a curated offline lookup, not runtime discovery; an `OWNER/REPOSITORY` argument installs an unlisted repository. Released-plugin installation selects a compatible HTTPS GitHub release asset, optionally verifies `checksums.txt`, writes the binary to `~/.grove/plugins/`, and records release metadata. `GITHUB_TOKEN` is used for GitHub API requests when supplied.

## Source-seeded creation and branch integration

`gw create` accepts optional source provenance:

```bash
gw create my-feature -b feat/login -r svc-a,svc-b \
  --source-url "https://github.com/org/repo/pull/42" \
  --source-provider github \
  --source-ref "42" \
  --source-title "Add login flow"
```

Grove persists an optional `source` object in `state.json` with `provider`, `url`, `ref`, and `title`. These values are metadata, not a provider API: core stores and displays them but never resolves GitHub, GitLab, Notion, Slack, or another provider-specific source. A plugin or caller performs provider-specific lookup and passes the resulting values to `gw create`. Missing `ref` and `title` are omitted by the state model; if neither source URL nor provider is supplied, the source object remains absent. Hooks receive URL, ref, and title as `{source_url}`, `{source_ref}`, and `{source_title}`. There is deliberately no `{source_provider}` placeholder.

```mermaid
sequenceDiagram
    participant Caller as User or plugin
    participant Create as gw create
    participant Hooks as lifecycle hooks
    participant Git as Git worktrees
    participant State as Grove state
    Caller->>Create: Name branch repos source metadata
    alt normal branch mode
        Create->>Hooks: resolve_branch with branch name and source values
        Hooks-->>Create: One valid branch line
    else --track
        Create->>Create: Skip resolve_branch
    end
    Create->>Git: Fetch selected repositories in parallel
    Create->>Git: Create worktrees sequentially
    Git-->>Create: Worktree paths or failure
    alt provisioning succeeds
        Create->>State: Commit workspace and source metadata
        Create->>Hooks: post_create
        Create-->>Caller: Success and workspace path side effect
    else provisioning fails
        Create->>Git: Roll back created worktrees and branches
        Create-->>Caller: Error
    end
```

*This sequence shows the command boundary for a source-seeded create; source resolution itself is intentionally external to Grove.*

### `resolve_branch`

`resolve_branch` is a special global hook configured in `~/.grove/config.toml`. The operation service runs it once before provisioning, and `gw create --replace` resolves it before deleting the current workspace. Its `{branch}` value is the branch as typed, `{name}` is the workspace name, and source placeholders are available; `{path}` is empty because the workspace does not exist. stdin is not attached.

The hook's stdout must trim to exactly one non-empty line, and that line must pass Git branch-name validation. stderr is never interpreted as the branch: it is streamed to stderr when `stream = true`, otherwise shown on failure. Any non-zero exit, timeout, empty or multi-line output, or invalid branch aborts creation regardless of `on_failure`; nothing is created. `--track` skips the resolver because the real remote branch name is used, and `--no-hooks` skips it and uses the typed branch. A later `gw add-repo` reuses the branch already stored in the workspace and does not resolve it again.

### `--track`

`gw create --track` checks out an existing `origin/<branch>` remote-tracking ref rather than creating a new branch from the base. Grove fetches before provisioning and uses `git worktree add --track -b <branch> <path> origin/<branch>` when that ref exists. Track mode falls back to normal branch creation for a repository where the remote branch is absent; when a workspace contains multiple repositories, `TrackBranchRepo` can restrict tracking to the source repository while sibling repositories receive fresh branches from their base.

Creation fetches repositories in parallel, then adds worktrees sequentially so a failure can roll back already-created worktrees and branches. The resulting paths, branch, and source metadata are committed to Grove state. Per-repository `base_branch` and setup/teardown/sync hooks come from `.grove.toml`; unknown keys are ignored by core so external consumers can use the same file.

## Lifecycle hooks

Global hooks are configured under `[hooks]` in `~/.grove/config.toml`:

| Hook | Boundary and timing |
|---|---|
| `resolve_branch` | Before create provisioning; its stdout selects the branch, with the strict contract above |
| `post_create` | After the workspace is provisioned and committed |
| `post_add_repo` | After `gw add-repo` adds at least one new repository; skipped when none was added |
| `pre_delete` | Before destructive worktree removal |
| `on_close` | From `gw go -c`; this hook is required for the close operation |

A hook may be a command string or a table with `command`, `description`, `stream`, `timeout`, and `on_failure`. Commands run through `sh -c`. `{name}`, `{path}`, `{branch}`, `{source_url}`, `{source_ref}`, and `{source_title}` are replaced with single-quoted values; embedded single quotes are escaped, and empty values become empty text. This prevents a branch or source title such as `feat/x; rm -rf ~` from becoming a second shell command.

By default hook output is captured and stays quiet on success. On failure captured output is line-prefixed with the hook name and written to stderr. `stream = true` sends prefixed output live to stderr. This keeps stdout safe for path capture. A positive Go `timeout` bounds execution; on supported macOS and Linux builds Grove kills the hook's process group, including children. Invalid durations are warned about and ignored. Ordinary hook failures warn and allow the operation to continue unless `on_failure = "abort"`; `post_create` failure does not roll back an already-created workspace, while an aborting `pre_delete` failure prevents removal. `--no-hooks` disables global hooks for that invocation but does not disable per-repository hooks.

Per-repository `.grove.toml` supports core keys `base_branch`, `setup`, `teardown`, `pre_sync`, and `post_sync`. Setup runs after creation, teardown before deletion, and sync hooks surround a successful rebase; their failures are warnings. `pre_run`, `run`, and `post_run` are read by the external `gw-run` plugin rather than supervised by Grove.

## Shell integration and stdout/stderr

`gw shell-init` prints a wrapper, and does not change the parent shell itself:

```bash
eval "$(gw shell-init)"
gw shell-init --shell nu | save -f ~/.config/nushell/grove.nu
```

The bash/zsh wrapper calls `command gw` to avoid recursion. `gw go` writes only the destination path to stdout without a trailing shell message; after a successful command the wrapper changes directory when that path exists, otherwise it prints the captured output. For `gw create`, the wrapper sets `GROVE_CD_FILE`; Grove writes the committed workspace path to that file and the wrapper changes directory after success. The Nushell wrapper implements the same `go` and `create` protocol using Nushell environment semantics. Hook output, navigation-time deletion messages, and diagnostics belong on stderr so they cannot corrupt `cd "$(gw go NAME)"` or another captured path.

Commands that support query output accept `--output` values `table`, `json`, `jsonl`, `tsv`, `name`, and `path` (and legacy `--json` maps to `json`; combining it with another output is an error):

| Format | stdout contract |
|---|---|
| `table` | Human-readable headings and rows; paths may abbreviate the home directory as `~`. Empty results may print a human message. |
| `json` | One indented JSON value, normally an array for list commands or an object for a single workspace. |
| `jsonl` | One compact JSON value per line; list commands emit one record per line, including no lines for an empty list. |
| `tsv` | Tab-separated rows with a header. Values use CSV/TSV escaping. |
| `name` | One workspace or repository name per line. |
| `path` | One workspace or repository path per line, suitable for shell pipelines. |

Machine consumers should select a machine format explicitly and treat stderr as diagnostics. `gw ws show`, workspace listing, status, and repository listing share these formats, but their JSON fields are the command's model-specific contract. For prune automation, `gw prune --json` emits an array; a failed deletion adds an `error` field to that candidate and the command returns non-zero after attempting all candidates.

## Git subprocess boundary and traces

Grove invokes the `git` executable with an explicit working directory, a process-wide environment captured at package initialization, and stdin connected to the OS null device. It always adds `GIT_TERMINAL_PROMPT=0`. Unless the caller already set `GIT_SSH_COMMAND`, it adds `GIT_SSH_COMMAND=ssh -o BatchMode=yes`. Fetch, branch, worktree, status, rebase, and registration-repair operations therefore fail rather than prompting for credentials in a non-interactive command.

Git's combined output is trimmed and returned through a structured `GitError` containing the command and output. Authentication-shaped failures add a credentials hint, but Grove does not print or persist credentials as a normal feature. With `--verbose`, Git calls are written to `~/.grove/grove.log` as debug traces containing the working directory, redacted command, elapsed time, success/failure, and truncated failure output. Credential-like URL userinfo (`scheme://user:pass@host`) is replaced with `***@` before trace truncation; this redaction deliberately masks usernames too. The log is a rotating 1 MB file with three backups. Info, warning, and error records are written regardless of `--verbose`; debug Git traces require it.

## Extension and change guidance

- Keep provider-specific parsing and API calls in a plugin or caller. Grove's `WorkspaceSource` is opaque provenance.
- Use the supplied plugin variables and Grove commands rather than writing `state.json` or inventing a plugin override of a built-in command.
- Preserve stdout for paths and machine formats. Put hook progress, human diagnostics, and deletion warnings on stderr.
- Treat `resolve_branch` as a precondition and `post_add_repo` as a postcondition: the former must be safe before destructive `--replace`, while the latter fires only after an actual repository addition.
- When changing Git invocation or trace code, maintain non-interactive credentials behavior and redact before truncating trace output.

Focused tests cover plugin discovery, argument forwarding, environment propagation, and removal in `e2e/plugin_test.go`; resolver output, `--track`, source placeholders, `post_add_repo`, and `--no-hooks` in lifecycle and operation tests; output formats in `cmd/query_output_test.go`; shell path behavior in `cmd/go_cmd_test.go` and workspace create tests; and Git non-interactive settings, trace gating, redaction, and truncation in `internal/gitops` tests. These tests are the safety net for changing a public integration contract.
