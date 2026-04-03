# Grove Go Implementation Spec

Complete specification derived from the Python implementation. This document serves as
the canonical reference for reimplementing Grove in Go.

---

## Implementation Status

**Last updated:** 2026-04-02

| Area | Status | Tests | Notes |
|---|---|---|---|
| Data Model | Done | 9 | All structs, JSON roundtrip, backward compat |
| Config (TOML) | Done | 13 | Load/save, migration, preset validation |
| State (JSON) | Done | 16 | Store struct, CRUD, find-by-path, atomic writes, error on corrupt |
| Git Operations | Done | 24 | Real git repos, auth detection, PRStatus (gh+glab), ReadGroveConfig cache |
| Discovery (shallow) | Done | 10 | Hidden dir filter, first-occurrence dedup |
| Discovery (deep) | Done | 10 | Symlink loop prevention, remote URL cache (mtime+TTL), batch parallel resolve, dedup |
| Discovery Remote Cache | Done | 16 | mtime+TTL dual invalidation, batch parallel fetch, injectable clock/mtime/fetcher |
| Workspace Create | Done | 10 | Parallel fetch, rollback, auto-branch, MCP config, CD file, setup hooks, progress output |
| Workspace Delete | Done | 5 | Parallel teardown, branch cleanup, teardown hooks, partial-failure preservation |
| Workspace Sync | Done | 7 | Parallel, rebase, conflict abort, dirty skip, pre/post hooks via RunCmdSilent |
| Workspace Status | Done | 5 | Parallel, JSON, verbose, PR (gh+glab), tabwriter tables |
| Workspace Rename | Done | 4 | State-first with rollback, created_at preserved |
| Workspace Add/Remove Repo | Done | 8 | Branch conflict, setup hooks on add, teardown on remove, ws-not-found |
| Doctor | Done | 5 | Missing worktree/dir, orphaned Claude memory (fast: no recursive walk), progress output |
| Stats + Heatmap | Done | 15 | Injectable clock, heatmap alignment, month label positioning, boundary tests |
| Claude Memory Sync | Done | 12 | Rehydrate, harvest, migrate, fast orphan detection (2-level ReadDir) |
| Hook Handler | Done | 17 | Handler struct, injectable pid/env/git/clock, session lifecycle, state machine |
| MCP Server | Done (e2e) | — | Tested via e2e (JSON-RPC: init, ping, tools, announce, get, list) |
| Update Check | Done | 9 | Checker struct, injectable clock/fetcher, background fetch verify, cache write test |
| Logging | Done | 4 | Rotating file, 1MB max, 3 backups |
| Console Output | Done | 7 | tabwriter tables (kubectl-style), uppercase headers, alignment tests |
| Interactive Pickers | Done | 6 | Bubbletea on stderr (pipeable stdout), terminal guard, single-choice auto-select |
| Shell Completions | Done | — | --repos, --preset, workspace names via Cobra |
| CLI Commands | Done | — | All 20+ commands with flags, pickers, confirmations |
| Workspace Service | Done | 57 | Service struct with injectable State/Stats/ClaudeDir/RunCmd/RunCmdSilent |
| gw dash TUI | Deferred | — | Planned as separate plugin/binary |
| gw run TUI | Partial | — | Inline with prefix output (no split-pane TUI) |
| Zellij Integration | Partial | — | gw go --close-tab works; full tab matching deferred to dash |

**Totals: 226 unit tests + 59 e2e tests = 285, all passing**

### Architecture: Testability

All packages use **dependency injection** via structs with injectable function fields:

| Package | Struct | Injected Dependencies |
|---|---|---|
| `workspace` | `Service` | `*state.Store`, `*stats.Tracker`, `ClaudeDir`, `WorkspaceDir`, `RunCmd`, `RunCmdSilent` |
| `stats` | `Tracker` | `StatsPath`, `NowFn` |
| `hook` | `Handler` | `StatusDir`, `NowFn`, `PidFn`, `EnvFn`, `GitBranchFn`, `GitDirtyFn` |
| `update` | `Checker` | `CachePath`, `NowFn`, `FetchLatestFn` |
| `state` | `Store` | `Path` |
| `discover` | (functions) | `fetcher`, `nowFn`, `mtimeFn` params |

No test mutates `config.GroveDir` (except `config_test.go` itself). All tests create isolated instances.

---

## Table of Contents

1. [Overview](#overview)
2. [Data Model](#data-model)
3. [File Formats & Storage](#file-formats--storage)
4. [Git Operations](#git-operations)
5. [Repository Discovery](#repository-discovery)
6. [Workspace Operations](#workspace-operations)
7. [CLI Commands](#cli-commands)
8. [Supporting Systems](#supporting-systems)
9. [Key Invariants](#key-invariants)
10. [Test Requirements](#test-requirements)

---

## Overview

Grove (`gw`) is a Git Worktree Workspace Orchestrator. It manages multi-repo
worktree-based workspaces so developers can spin up isolated branches across
several repos at once.

### Architecture Layers

```
CLI (Cobra)
  -> config (TOML load/save)
  -> state (JSON persistence)
  -> workspace (orchestration)
       -> git (subprocess wrappers)
       -> discover (repo scanning + URL cache)
       -> stats (event log)
       -> claude (memory sync)
       -> mcp (cross-workspace communication)
```

### Concurrency Model

All concurrency uses goroutines (Python uses `ThreadPoolExecutor`). The core
parallel helper must:
- Accept a list of `(name, value)` work items
- Return results in **original input order** (not completion order)
- Capture errors per-item — never propagate panics
- Optionally show a spinner/progress indicator

---

## Data Model

### Config

```go
type Config struct {
    RepoDirs        []string            // directories containing git repos
    WorkspaceDir    string              // root for workspace directories
    Presets         map[string][]string // named repo groups
    ClaudeMemSync   bool                // sync .claude/ memory dirs
}
```

### RepoWorktree

```go
type RepoWorktree struct {
    RepoName     string // folder name of source repo
    SourceRepo   string // absolute path to main checkout
    WorktreePath string // absolute path to worktree
    Branch       string // branch name
}
```

### Workspace

```go
type Workspace struct {
    Name      string         // also used as directory name
    Path      string         // workspace_dir/name
    Branch    string         // shared branch across all repos
    Repos     []RepoWorktree
    CreatedAt string         // ISO 8601 timestamp
}
```

### RepoInfo (discovery)

```go
type RepoInfo struct {
    Name        string // folder name
    Path        string // absolute path
    Remote      string // origin URL (may be empty)
    DisplayName string // "owner/repo" or folder name
}
```

### DoctorIssue

```go
type DoctorIssue struct {
    WorkspaceName string
    RepoName      string // may be empty for workspace-level issues
    Issue         string
    SuggestedAction string
}
```

---

## File Formats & Storage

All files live under `~/.grove/` (the "GROVE_DIR").

### `config.toml` — Global Configuration

```toml
repo_dirs = ["/path/a", "/path/b"]
workspace_dir = "/path/to/workspaces"
claude_memory_sync = true  # only written if true

[presets.backend]
repos = ["api", "worker"]

[presets.frontend]
repos = ["web-app", "design-system"]
```

**Backward compat:** Old format used singular `repos_dir = "/single/path"`. On load,
wrap in a list and rewrite the file.

**Preset name validation:** Must match `^[a-zA-Z0-9_-]+$`.

### `state.json` — Workspace State

```json
[
  {
    "name": "my-feature",
    "path": "/Users/foo/.grove/workspaces/my-feature",
    "branch": "feat/my-feature",
    "repos": [
      {
        "repo_name": "api",
        "source_repo": "/Users/foo/dev/api",
        "worktree_path": "/Users/foo/.grove/workspaces/my-feature/api",
        "branch": "feat/my-feature"
      }
    ],
    "created_at": "2024-01-15T10:30:00.123456"
  }
]
```

**Corrupt file handling:** If JSON is invalid, exit with a helpful message
directing user to `gw doctor --fix`.

**Backward compat:** `repos` defaults to `[]` if absent. `created_at` defaults
to `""` if absent.

### `stats.json` — Usage Event Log

Append-only JSON array:

```json
[
  {
    "event": "workspace_created",
    "timestamp": "2024-01-15T10:30:00",
    "workspace_name": "my-feature",
    "branch": "feat/my-feature",
    "repo_names": ["api", "worker"],
    "repo_count": 2
  }
]
```

Events: `workspace_created`, `workspace_deleted`. Stats failures must never
propagate — always swallow errors.

### `cache/remotes.json` — Discovery Cache

```json
{
  "/resolved/path/to/repo": {
    "url": "git@github.com:owner/repo.git",
    "mtime": 1705312200.0,
    "ts": 1705312200.0
  }
}
```

Cache invalidation: both `.git/config` mtime must match AND 24h TTL must not
have expired. Empty string `url` = repo with no remote.

### `update-check.json` — Version Check Cache

```json
{
  "last_check": 1705312200,
  "latest": "0.12.13"
}
```

24h TTL. Background goroutine fetches `https://api.github.com/repos/nicksenap/grove/releases/latest`.
Always return immediately using cached value; update cache in background for next invocation.

### `status/<session_id>.json` — Agent Dashboard State

One file per active Claude Code session. Written by `gw _hook`. Session IDs must
be validated against `^[a-zA-Z0-9_-]+$` (path traversal prevention).

### `messages.db` — MCP Announcement Store

SQLite with WAL mode. Single `announcements` table:

```sql
CREATE TABLE announcements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    category TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_repo_created ON announcements(repo_url, created_at);
```

Valid categories: `breaking_change`, `status`, `warning`, `info`.
Prune rows older than 30 days on open.

### Atomic Writes

ALL file writes (state, config, stats, cache) must use atomic write:
1. Create temp file in same directory
2. Write content
3. `os.Rename(tmp, target)` (atomic on POSIX)
4. On error: clean up temp file, return original error

---

## Git Operations

All git calls go through a single `run` helper that:
- Sets `GIT_TERMINAL_PROMPT=0`
- Sets `GIT_SSH_COMMAND=ssh -o BatchMode=yes` (preserving existing value)
- Uses `stdin=devnull`
- On auth errors (permission denied, host key verification, terminal prompts disabled),
  returns a detailed user-facing message with remediation steps

### Function Reference

| Function | Git Command | Returns | Error behavior |
|---|---|---|---|
| `RemoteURL(path, remote)` | `git remote get-url <remote>` | `string, error` | Returns `""` on any error (never fatal) |
| `ParseRemoteName(url)` | pure string parsing | `string` | `"owner/repo"` from SSH/HTTPS URL, `""` if unparseable |
| `IsGitRepo(path)` | `git rev-parse --git-dir` | `bool` | |
| `BranchExists(repo, branch)` | `git rev-parse --verify refs/heads/<branch>` | `bool` | |
| `Fetch(repo)` | `git fetch --all --quiet` | `error` | |
| `DefaultBranch(repo)` | `git symbolic-ref refs/remotes/origin/HEAD --short` | `string, error` | Falls back to probing `origin/main` then `origin/master` |
| `ReadGroveConfig(path)` | reads `.grove.toml` | `map[string]any, error` | Cache per process (sync.Once or similar) |
| `RepoBaseBranch(repo)` | reads `.grove.toml` `base_branch` | `string` | Returns `"origin/<base>"` or `""` |
| `ResolveBaseBranch(repo)` | `.grove.toml` > `DefaultBranch()` | `string, error` | |
| `RepoHookCommands(sourceRepo, hook)` | reads `.grove.toml` | `[]string` | Single string wrapped in slice |
| `CreateBranch(repo, branch, startPoint)` | `git branch <branch> [<start>]` | `error` | |
| `DeleteBranch(repo, branch, force)` | `git branch -d/-D <branch>` | `error` | |
| `WorktreeAdd(repo, path, branch)` | `git worktree add <path> <branch>` | `error` | |
| `WorktreeRemove(repo, path)` | `git worktree remove <path> --force` | `error` | Always `--force` |
| `WorktreeRepair(repo, path)` | `git worktree repair <path>` | `error` | |
| `WorktreeList(repo)` | `git worktree list --porcelain` | `[]WorktreeEntry` | Parse porcelain format |
| `WorktreeHasBranch(repo, branch)` | uses `WorktreeList` | `bool, error` | |
| `RepoStatus(path)` | `git status --short` | `string` | |
| `CurrentBranch(path)` | `git branch --show-current` | `string` | |
| `RebaseOnto(path, base)` | `git rebase <base>` | `error` | |
| `RebaseAbort(path)` | `git rebase --abort` | `error` | |
| `CommitsAheadBehind(path, upstream)` | `git rev-list --left-right --count <upstream>...HEAD` | `(ahead, behind int, err)` | left=behind, right=ahead |
| `PRStatus(path)` | `gh pr view --json number,state,reviewDecision` | `map, error` | Returns nil if `gh` not in PATH or on error |

### `.grove.toml` Per-Repo Config

```toml
base_branch = "stage"           # override default branch for rebasing
setup = "npm install"           # or list: setup = ["npm install", "npm run build"]
teardown = "make clean"
run = "npm start"
pre_sync = "npm run pre-sync"
post_sync = "npm run post-sync"
pre_run = "echo starting"
post_run = "echo done"
```

Hooks are read from the **source repo** but executed with cwd set to the **worktree path**.

---

## Repository Discovery

### Shallow Discovery (`FindRepos`, `FindAllRepos`)

- One level deep scan of configured `repo_dirs`
- Check for `.git` directory existence (no subprocess)
- Skip hidden directories (starting with `.`)
- First occurrence wins on name collision across multiple dirs

### Deep Discovery (`DiscoverRepos`)

Three phases:

1. **Filesystem scan**: Recursive walk up to `max_depth=3`. Track seen paths
   (resolved) to avoid symlink loops. Don't descend into discovered repos.

2. **Batch remote URL resolution**: Load cache from disk. For cache misses,
   resolve in parallel (16 goroutines). Save updated cache.

3. **Dedup and sort**: Same remote URL = same repo. Direct children of configured
   dirs win over deeply nested repos. Sort alphabetically by display name.

---

## Workspace Operations

### Create Workspace

```
create_workspace(name, repo_paths, branch, config) -> Workspace | error
```

1. Check name uniqueness in state
2. Create workspace directory: `config.workspace_dir/name`
3. Provision worktrees (see below)
4. Save to state
5. Record stats event (swallow errors)
6. Rehydrate Claude memory (if enabled)
7. Write `.mcp.json` to workspace root + each worktree dir

### Provision Worktrees (shared by create + add-repo)

```
provision_worktrees(repo_paths, branch, workspace_path) -> []RepoWorktree | error
```

1. **Branch collision check (parallel)**: `WorktreeHasBranch` for each repo.
   Abort if any repo already has a worktree on the branch.
2. **Fetch (parallel, non-fatal)**: Fetch failures emit warning only.
3. **Sequential worktree creation with rollback**: For each repo:
   - If branch doesn't exist locally: create from resolved base branch
   - `WorktreeAdd`
   - On failure: rollback all created worktrees, return error
4. **Setup hooks (parallel, non-fatal)**: Run `.grove.toml` `setup` commands.

### Delete Workspace

```
delete_workspace(name) -> error
```

1. Load workspace from state
2. Harvest Claude memory (if enabled) — copy back to source repos
3. Remove `.mcp.json` entries
4. Parallel teardown+remove for each repo:
   - Run `teardown` hook (suppressed)
   - `WorktreeRemove`
   - On failure with `force_cleanup`: `rmtree` the worktree dir
   - Delete branch (safe `-d`, warn on unmerged)
5. Remove workspace directory
6. Remove from state **only if** zero failures or dir is gone
7. Record stats event

### Sync Workspace

```
sync_workspace(workspace) -> []SyncResult
```

Per repo (parallel):
1. Fetch (suppress errors)
2. Resolve base branch
3. If dirty (non-empty `git status`): skip
4. If `behind == 0`: "up to date"
5. Run `pre_sync` hook
6. `RebaseOnto`. On success: run `post_sync` hook, return "rebased (N commits)".
   On error: `RebaseAbort`, return "conflict"

### Workspace Status

```
workspace_status(workspace) -> []RepoStatus
```

Per repo (parallel): returns `{repo, branch, status, ahead, behind}`.
- `status` = "clean" when `git status --short` is empty
- `ahead`/`behind` = "-" on any error

### All Workspaces Summary

```
all_workspaces_summary() -> []WorkspaceSummary
```

Parallel per workspace, each internally parallel per repo. Returns
`{name, branch, repos_count, status_summary, path}`.

### Rename Workspace

```
rename_workspace(old, new, config) -> error
```

1. Validate: old exists, new doesn't exist (state or filesystem)
2. Mutate workspace object in memory (name, path, all worktree paths)
3. **State-first**: write updated state with `match_name=old`
4. OS rename directory. On failure: **rollback state**
5. `WorktreeRepair` for each repo (suppressed)
6. Migrate Claude memory dirs (if enabled, suppressed)

### Add Repo to Workspace

```
add_repo_to_workspace(ws, repo_paths, config) -> []RepoWorktree | error
```

- Filter out already-present repos
- Call `provision_worktrees` with `remove_workspace_dir_on_rollback=false`
- Append new repos to workspace, update state

### Remove Repo from Workspace

```
remove_repo_from_workspace(ws, repo_names, force) -> error
```

- Parallel teardown+remove (with `delete_branch=true`)
- Only remove successfully-removed repos from state
- Return error if any removal failed

### Doctor / Diagnose

```
diagnose_workspaces(config) -> []DoctorIssue
```

Checks:
1. Orphaned Claude memory directories (if enabled)
2. Workspace directory exists
3. Source repo exists for each repo entry
4. Worktree path exists for each repo entry
5. Worktree is registered in git (`WorktreeList`)

### Fix Issues

```
fix_workspace_issues(issues) -> int
```

- "remove stale state" -> remove workspace from state
- "remove stale repo" -> remove repo entry from workspace
- "orphaned Claude memory" -> cleanup dirs
- Skip repo-level fixes for workspaces being fully removed

---

## CLI Commands

### Global Options

| Flag | Type | Default | Description |
|---|---|---|---|
| `--version` / `-v` | bool | false | Print version and exit |
| `--verbose` | bool | false | Enable debug logging to `~/.grove/grove.log` |

### Top-Level Commands

#### `gw init [dirs...]`
Create or merge Grove config. Scans dirs for repos. Creates default workspace dir.
Exit 1 if any dir doesn't exist.

#### `gw add-dir <path>`
Append directory to `repo_dirs`. Skip if already configured. Exit 1 if doesn't exist.

#### `gw remove-dir [path]`
Remove directory from config. **Interactive picker** if path omitted.

#### `gw explore`
Deep scan all configured dirs. Group results by source dir. Mark nested repos.

#### `gw create [name] [--repos/-r] [--branch/-b] [--preset/-p] [--all] [--copy-claude-md/--no-copy-claude-md]`
Create workspace. Complex interactive flow when args omitted:
- Branch: prompt
- Repos: preset picker or multi-select, with offer to save as preset
- Claude MD: confirm prompt if file exists
- Auto-cd via `GROVE_CD_FILE` env var

#### `gw list [name] [--status/-s] [--json/-j]`
Three modes:
1. Detail view (name given): name, branch, path, created, repo table
2. Status view (`--status`): table with git status summary
3. Plain list (default): table with name, branch, repos, path, created

#### `gw delete [name] [--force/-f]`
Delete workspace(s). **Multi-select picker** if name omitted.
Confirm unless `--force`.

#### `gw doctor [--fix] [--json/-j]`
Diagnose workspace health. Show issues table. `--fix` auto-repairs.

#### `gw stats`
Heatmap (52-week GitHub-style), summary stats, top repos table.

#### `gw rename [name] --to <new>`
Rename workspace. **Picker** if name omitted.

#### `gw add-repo [name] [--repos/-r]`
Add repos to existing workspace. **Pickers** for workspace and repos if omitted.

#### `gw remove-repo [name] [--repos/-r] [--force/-f]`
Remove repos from workspace. **Pickers** if omitted. Confirm unless `--force`.

#### `gw status [name] [--verbose/-V] [--pr/-P] [--all/-a] [--json/-j]`
Show workspace git status. Auto-detect workspace from cwd.
- `--pr`: concurrent `gh pr view` calls, color-coded states
- `--all`: deprecated, redirect to `gw list -s`
- `--verbose`: raw `git status` for dirty repos

#### `gw sync [name]`
Fetch + rebase each repo onto base branch. Auto-detect from cwd.

#### `gw run [name]`
Launch TUI with per-repo processes from `.grove.toml` `run` hooks.

#### `gw go [name] [--back/-b] [--delete/-d] [--close-tab/-c]`
Navigation. Prints path for shell `cd`. Shell function intercepts output.
- `--back`: navigate to source repo dir
- `--delete`: async delete + navigate away
- `--close-tab`: close Zellij pane

#### `gw shell-init`
Print shell integration function to stdout. `eval "$(gw shell-init)"`.

### `gw preset` Subcommands

#### `gw preset add [name] [--repos/-r]`
Create/overwrite preset. **Pickers** if omitted.

#### `gw preset list`
Table of presets.

#### `gw preset remove [name]`
Delete preset. **Picker** if omitted.

### `gw dash` Subcommands

#### `gw dash` (bare)
Launch Textual-equivalent agent monitoring dashboard TUI.

#### `gw dash install [--dry-run]`
Install Claude Code hooks into `~/.claude/settings.json`.

#### `gw dash uninstall`
Remove Grove hooks from `~/.claude/settings.json`.

### Hidden Commands

#### `gw mcp-serve --workspace <name>`
Start MCP server (stdio transport). Three tools: `announce`, `get_announcements`,
`list_workspaces`.

#### `gw _hook --event <type>`
Handle Claude Code lifecycle events. Reads JSON from stdin.

---

## Supporting Systems

### Console Output

Two output streams:
- **stdout**: tables, JSON, paths (for piping)
- **stderr**: status messages (error, success, info, warning)

Message formats:
- `error: <msg>` (bold red)
- `ok: <msg>` (bold green)
- `info: <msg>` (dim)
- `warn: <msg>` (bold yellow)

### Claude Code Memory Sync

Memory dirs: `~/.claude/projects/<encoded-path>/memory/`

Path encoding: replace `/` and `.` with `-`.

| Operation | Direction | Behavior |
|---|---|---|
| Rehydrate | source -> worktree | Copy files that don't exist in worktree |
| Harvest | worktree -> source | Copy new/newer files (by mtime) back |
| Migrate | rename | Rename project dir, merge if target exists |
| Cleanup | audit | Remove dirs for non-existent worktrees |

### Version Check

Non-blocking. Check GitHub releases API in background goroutine.
Cache result for 24h. One-invocation lag by design.

### Stats / Heatmap

GitHub-style 52-week contribution grid. 7 rows (Mon-Sun) x N columns (weeks).
5 intensity levels. Brown-to-orange palette.

Computed stats: total created/deleted, active count, avg lifetime,
top 5 repos, created this week/month.

### MCP Server

FastMCP equivalent — JSON-RPC over stdio. Three tools:
- `announce(repo_url, category, message)` — insert announcement
- `get_announcements(repo_url, since?)` — query, excluding own workspace
- `list_workspaces()` — return all active workspaces

URL normalization: SSH and HTTPS both resolve to `owner/repo`.

### Dashboard Hook System

13 event types: `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `Stop`,
`SessionStart`, `SessionEnd`, `Notification`, `PermissionRequest`,
`UserPromptSubmit`, `SubagentStart`, `SubagentStop`, `PreCompact`, `TaskCompleted`.

State machine per session with statuses: PROVISIONING, IDLE, WORKING,
WAITING_PERMISSION, WAITING_ANSWER, ERROR, DONE.

### TUI (gw run)

Split-pane: sidebar with repo list + status dots, log pane showing selected
repo's output. Key bindings: q (quit), r (restart), j/k (nav), 1-9 (jump).

### Dashboard TUI (gw dash)

Kanban board: Active | Attention | Idle | Done columns. Agent detail panel.
Key bindings: h/l (columns), j/k (cards), enter (jump to Zellij tab),
y/n (approve/deny), / (search), r (refresh).

### Zellij Integration

Tab navigation, text input, approve/deny permission prompts.
5-tier tab matching strategy for jump-to-agent.

### Logging

Rotating file log at `~/.grove/grove.log`. 1 MB max, 3 backups.
Format: `YYYY-MM-DD HH:MM:SS LEVEL grove.module - message`.

---

## Key Invariants

1. **Branch uniqueness per repo**: Git only allows one worktree per branch.
   Always check before creation.

2. **Workspace name == directory name**: `workspace.path` is always
   `config.workspace_dir / workspace.name`.

3. **RepoWorktree path**: always `workspace.path / repo_name`.

4. **Atomic writes**: ALL file writes (state, config, stats, cache) use
   temp-file + `os.Rename`.

5. **State-first rename**: State is updated before filesystem rename,
   with explicit rollback on OS rename failure.

6. **Partial-failure preservation**: Delete keeps state on partial failure.
   Remove-repo only removes successfully-removed repos from state.

7. **Hook source vs execution**: Hooks read from `.grove.toml` in source repo,
   executed with cwd = worktree path.

8. **ReadGroveConfig is cached**: Per-process cache (never invalidated during
   a single command invocation).

9. **Result ordering**: Parallel operations return results in original input
   order, not completion order.

10. **Stats never fatal**: All stats recording is wrapped — failures never
    propagate to the user.

11. **Console output separation**: Status messages go to stderr. Data (tables,
    JSON, paths) goes to stdout. This enables piping.

---

## Test Requirements

### Unit Tests Needed

Based on the Python test suite, the Go implementation needs:

#### State (`state_test.go`)
- Load empty state
- Add and get workspace
- Get nonexistent returns nil
- Remove workspace
- Remove nonexistent is no-op
- Find by exact path
- Find by subdirectory path
- Find by unrelated path returns nil
- Multiple workspaces coexist
- State persists as valid JSON
- Corrupt JSON gives helpful error
- Atomic write produces valid JSON

#### Models (`models_test.go`)
- Config to_dict/from_dict roundtrip
- Config with presets roundtrip
- Config backward compat (singular repos_dir)
- Config omits empty presets
- RepoWorktree roundtrip
- Workspace roundtrip with nested repos
- Workspace tolerates missing repos/created_at keys

#### Config (`config_test.go`)
- Save and load roundtrip
- Save and load with presets
- Saved file is valid TOML
- Backward compat: old format loads
- Auto-migration rewrites file
- Multiple repo_dirs
- Returns nil when absent
- Preset name validation (valid and invalid)
- require_config exits when no config

#### Git (`git_test.go`)
- All functions with mocked subprocess calls
- Auth error detection and messaging
- SSH environment variable handling
- Porcelain output parsing
- PR status parsing with gh

#### Discover (`discover_test.go`)
- Shallow scan finds repos, skips hidden dirs
- Aggregate multiple dirs, first-occurrence wins
- Deep scan with grouping
- Max depth respected
- Remote URL dedup (direct children preferred)
- Cache hit avoids subprocess calls
- Parallel resolution achieves concurrency

#### Workspace (`workspace_test.go`)
- Create: success, duplicate name, duplicate branch, rollback on failure
- Create: auto-creates branch from base
- Setup hooks: single, multiple, missing
- Delete: success, not found, partial failure preserves state
- Sync: up to date, rebased, conflict (abort), dirty skip, unknown base
- Status: clean, modified, error handling, ahead/behind
- Parallel: all repos processed, error isolation, order preserved
- Lifecycle hooks: teardown order, pre/post sync
- Add repo: success, already present, branch conflict, rollback
- Remove repo: success, partial failure
- Rename: success, not found, name taken, dir exists, rollback, created_at preserved
- All workspaces summary
- Doctor: healthy, issues detected
- Fix: stale state removed, stale repo removed

#### Stats (`stats_test.go`)
- Record events, compute stats
- Average lifetime calculation
- Top repos ranking
- Weekly/monthly counts
- Corrupt file handled gracefully
- Duration formatting
- Heatmap generation

#### Claude Memory (`claude_test.go`)
- Path encoding
- Rehydrate: copies missing files, skips existing
- Harvest: copies new, overwrites older, preserves newer
- Migrate: rename, merge when target exists
- Orphan detection and cleanup

#### MCP Store (`mcp_store_test.go`)
- Table creation, WAL mode
- Insert and query
- Category validation
- Self-exclusion
- URL normalization (SSH/HTTPS cross-matching)
- Retention pruning

#### MCP Server (`mcp_server_test.go`)
- Announce tool
- Get announcements (with since filter, self-exclusion)
- List workspaces

#### Dashboard Hook (`hook_test.go`)
- Session lifecycle (start creates, end deletes)
- Session ID validation (path traversal prevention)
- Tool events (status transitions, counters)
- Permission/notification state machine
- Subagent counting
- Tool summary formatting
- Bootstrap (first event without SessionStart)

#### Dashboard Installer (`installer_test.go`)
- Install creates settings
- Install preserves existing hooks
- Install updates existing grove hook (no duplicates)
- Uninstall removes grove entries

### E2E Tests

The existing `go/e2e/run.sh` is the primary acceptance test. It covers:
- Init, create, list, go, status, add-repo, remove-repo, rename, sync,
  doctor, doctor --fix, delete, presets, stats, shell-init, explore,
  auto-detect from cwd, run hooks, _hook events, MCP server JSON-RPC

### Test Infrastructure

Tests need a fixture equivalent to Python's `tmp_grove`:
- Temp directory for GROVE_DIR, CONFIG_PATH, WORKSPACE_DIR, STATE_PATH
- Minimal config.toml
- Empty state.json
- Cleanup after each test

For git tests, either mock subprocess or use real git repos in temp dirs.

### Coverage Gaps in Python (opportunity for Go)

These are untested in Python — consider adding coverage:
1. Version check (`update.go`)
2. Console output formatting
3. Interactive picker paths
4. TUI process execution
5. Discovery cache TTL expiry
6. Doctor diagnostic logic (unit level)
7. Rename with Claude memory migration integration
