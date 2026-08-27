---
type: "Reference"
title: "Architecture"
description: "Architecture of Grove's CLI, workspace orchestration, Git operation wrappers, persisted state, repository discovery, lifecycle hooks, and plugins."
tags: [grove, architecture, cli, workspaces, git]
---

# Architecture

Grove is designed as a thin orchestrator around git worktrees. All state is persisted to disk and can be safely reconstructed; the tool is stateless except for its local configuration.

## High-Level Design

```
User Command (gw create, gw delete, gw status, etc.)
    ↓
Cobra CLI Handler (cmd/*.go)
    ↓
workspace.Service (core orchestration)
    ├─ Uses gitops wrappers for git operations
    ├─ Reads/writes state to ~/.grove/state.json
    ├─ Calls lifecycle hooks before/after operations
    └─ Uses goroutines for concurrent multi-repo operations
    ↓
Git Subprocess (git worktree, git checkout, git fetch, etc.)
```

## Layered Architecture

### 1. **Entry Point** (`cmd/gw/main.go`)
- Thin entry point that calls `cmd.Execute()` (Cobra's main entrypoint)
- Version and build info set by goreleaser via `-ldflags`

### 2. **CLI Layer** (`cmd/`)
Cobra command handlers that:
- Parse user input and flags
- Load global config (`~/.grove/config.toml`)
- Discover available repos
- Validate inputs (e.g., workspace exists, repos are registered)
- Call `workspace.Service` methods
- Handle user interaction (interactive pickers, confirmations)
- Run lifecycle hooks

**Key commands**:
- `cmd/create.go` — Create a workspace with specified repos
- `cmd/delete.go` — Destroy workspace (clean up worktrees + branches)
- `cmd/status.go` — Show git status across repos
- `cmd/sync_cmd.go` — Rebase all repos
- `cmd/reset.go` — Return repos to their recorded workspace branch, then sync
- `cmd/add_repo.go`, `cmd/remove_repo.go` — Modify existing workspace
- `cmd/preset.go` — Manage presets (named repo groups)

### 3. **Core Layer** (`internal/workspace/`)
**`workspace.Service`** is the orchestrator:
- `Create()` / `CreateWithOpts()` — Create a workspace with worktrees
- `Delete()` — Clean up worktrees and branches
- `Status()` — Query git status across repos (concurrent)
- `Sync()` — Rebase all repos onto their base branches
- `Reset()` — Switch repos to their recorded workspace branches, then sync
- `AddRepo()`, `RemoveRepo()` — Modify existing workspace

Uses goroutines to parallelize git operations (e.g., `Status()` runs `git status` in all repos concurrently).

### 4. **Data Layers**

#### Models (`internal/models/`)
Core data structures serialized to JSON:

```go
type Workspace struct {
    Name      string           // e.g. "feat-login"
    Path      string           // e.g. ~/.grove/workspaces/feat-login
    Branch    string           // e.g. "feat/login"
    CreatedAt string           // ISO 8601 timestamp
    Repos     []RepoWorktree   // Slice of repos in this workspace
    Source    *WorkspaceSource // Optional: GitHub PR, Notion page, etc.
}

type RepoWorktree struct {
    RepoName     string // e.g. "svc-api"
    SourceRepo   string // e.g. "~/.grove/source-repos/svc-api"
    WorktreePath string // e.g. ~/.grove/workspaces/feat-login/svc-api
    Branch       string // e.g. "feat/login"
}

type Config struct {
    RepoDirs     []string          // Directories to scan for repos
    WorkspaceDir string            // Where to create workspace directories
    Presets      map[string]Preset // Saved repo groups
    Hooks        map[string]Hook   // Lifecycle hooks
}

type Preset struct {
    Repos []string // List of repo names
}

type Hook struct {
    // Can be either a bare string or a table with metadata
    Command     string
    Description string
    Stream      bool          // Show output live
    Timeout     string        // Duration string (e.g., "5m")
    OnFailure   string        // "warn" (default) or "abort"
}

type WorkspaceSource struct {
    Provider string // e.g. "github", "gitlab", "notion"
    URL      string // Original source URL
    Ref      string // Provider-specific ref (e.g., PR number)
    Title    string // Human-readable title
}
```

#### State (`internal/state/`)
Persists workspace list to `~/.grove/state.json` using atomic writes:
- `GetWorkspace()` — Load a workspace by name
- `SaveWorkspace()` — Persist a workspace (create or update)
- `DeleteWorkspace()` — Remove from state
- `ListWorkspaces()` — Get all workspaces
- Thread-safe: uses a mutex for concurrent reads/writes

#### Config (`internal/config/`)
Loads and validates `~/.grove/config.toml`:
- `Load()` — Parse TOML config file
- Auto-migrates legacy `repos_dir` → `repo_dirs`
- Provides default `workspace_dir` if not specified
- Constants: `GroveDir`, `ConfigPath`, `DefaultWorkspaceDir`

### 5. **Integration Layers**

#### Git Operations (`internal/gitops/`)
Subprocess wrappers around git commands:
- `Clone()` — Clone a remote repo
- `CreateWorktree()` — Create a new worktree
- `Checkout()` — Check out a branch
- `Switch()` — Switch a worktree to a branch, optionally discarding tracked changes
- `Fetch()` — Fetch latest refs
- `Status()` — Get short git status
- `IsGitURL()` — Validate git URL format
- `ReadGroveConfig()` — Parse per-repo `.grove.toml`

Sets `GIT_TERMINAL_PROMPT=0` to disable interactive prompts and `GIT_SSH_COMMAND=ssh -o BatchMode=yes` to fail fast on auth errors.

#### Lifecycle Hooks (`internal/lifecycle/`)
Fires hooks at key moments:
- `Run(hookName, vars)` — Execute a named hook if configured
- Supports placeholders: `{name}`, `{path}`, `{branch}`, `{source_url}`, `{source_ref}`, `{source_title}`
- Handles hook output: quiet capture by default, streaming with `stream = true`
- Enforces timeouts and `on_failure` policies
- Skipped entirely with `--no-hooks` flag or if hook not configured

#### Repository Discovery (`internal/discover/`)
Finds git repos in configured directories:
- `DiscoverReposWithCache(dirs)` — Recursively scan up to three levels deep
- Returns sorted `RepoInfo` values with local and remote identities
- Deduplicates clones by remote URL, preferring direct children over nested clones
- Caches remote URL resolution while rescanning the filesystem for each command

#### Plugin System (`internal/plugin/`)
Manages external commands:
- `Install()` — Download latest release from GitHub
- `Upgrade()` — Re-fetch all installed plugins
- `Remove()` — Uninstall a plugin
- Stores plugin metadata in `~/.grove/plugins/`
- Plugins are exec'd from PATH or `~/.grove/plugins/`

### 6. **UI Layers**

#### Interactive Picker (`internal/picker/`)
Terminal-based menu selection:
- `PickOne()` — Single-select with type-to-search
- `PickMany()` — Multi-select with arrow + tab + `(all)` shortcut
- Used for interactive preset/repo selection

#### Console Output (`internal/console/`)
Colored output helpers:
- `Infof()`, `Successf()`, `Warningf()`, `Errorf()`
- Table rendering for workspace listings
- Consistent formatting

#### Logging (`internal/logging/`)
Structured debug logging:
- `Setup(verbose)` — Initialize logging level
- `Info()`, `Debug()`, `Error()` — Log at appropriate levels
- Disabled by default; enabled with `--verbose` flag

## Data Flow Example: `gw create my-feature -b feat/login -r svc-a,svc-b`

1. **cmd/create.go**
   - Parses `-b` (branch), `-r` (repos) flags
   - Loads config from `~/.grove/config.toml`
   - Discovers all repos via `discover.DiscoverReposWithCache()`
   - Validates that `svc-a` and `svc-b` exist
   - Calls `workspace.Service.Create()`

2. **internal/workspace/create.go** (through `workspace.Service`)
   - Checks if `my-feature` already exists (error if so)
   - For each repo (`svc-a`, `svc-b`):
     - Calls `gitops.CreateWorktree(sourceRepo, workspacePath, branchName)`
     - If branch doesn't exist: creates it from the base branch
     - If branch exists remotely: checks out the tracking branch
   - Runs per-repo setup commands from `.grove.toml`
   - Creates `Workspace` struct and persists to state
   - Fires `post_create` hook with placeholders

3. **internal/gitops/gitops.go**
   - Executes `git worktree add /path/to/workspace/svc-a feat/login`
   - Handles auth errors and retry logic
   - If branch not found, executes `git checkout -b feat/login origin/main`

4. **internal/state/state.go**
   - Atomically writes updated workspace list to `~/.grove/state.json`

5. **internal/lifecycle/lifecycle.go**
   - Loads hook from config
   - Expands placeholders (e.g., `{path}` → workspace directory)
   - Executes hook command via `sh -c` (with `stream` or silent capture)
   - Logs hook failures (or aborts if `on_failure = "abort"`)

## Key Design Decisions

### 1. **Single Static Binary**
All functionality is compiled in; no runtime plugins or external tools required (except `git`). This makes Grove trivial to install and distributes easily.

### 2. **Stateless Orchestrator**
Grove doesn't manage the git repositories or worktrees directly. It:
- Uses `git worktree` for isolation (not symlinks or mounts)
- Persists workspace metadata but never modifies it (state is read-write)
- Can reconstruct all state by re-scanning the filesystem
- Allows manual git operations alongside Grove

### 3. **Goroutine-Based Concurrency**
Multi-repo operations (status, sync) use goroutines to parallelize git calls. No external job queue or message bus — just lightweight Go concurrency.

### 4. **Atomic State Writes**
State is written atomically (write-to-temp, fsync, rename) to prevent corruption if Grove crashes mid-operation.

### 5. **Lifecycle Hooks as First-Class**
Hooks are not an afterthought; they're central to the design:
- Enable plugins (external commands called via hooks)
- Support shell integration (terminal multiplexer panes)
- Allow automation (CI/CD on workspace creation)

### 6. **Workspace Operations Are Split by Responsibility**

The workspace service remains the orchestration boundary, but its operations are implemented in focused files such as `internal/workspace/create.go`, `status.go`, `sync.go`, `remove.go`, `rename.go`, and `reset.go`. This keeps command-specific lifecycle behavior close to its implementation while preserving one service API for the CLI. The reset path uses `internal/gitops.Switch()` to restore each recorded branch before reusing the existing sync operation.

### 7. **Plugin Extensibility**
Rather than embedding tool-specific logic for coding agents, terminal multiplexers, or dashboards, Grove exposes hooks and environment variables. Plugins decide what to do with workspace lifecycle events.

### 8. **Per-Repo Configuration**
Each repo can have its own `.grove.toml` to define:
- Default base branch (override main/master)
- Setup commands to run after worktree creation

This enables a single Grove config to work across diverse monorepo structures.

## Concurrency Model

Grove uses Go's lightweight goroutines for concurrent git operations:

```go
// Example: Status across 10 repos
var wg sync.WaitGroup
results := make(chan StatusResult, len(repoNames))
for _, repo := range repoNames {
    wg.Add(1)
    go func(r string) {
        defer wg.Done()
        status, err := gitops.Status(workspacePath + "/" + r)
        results <- StatusResult{Repo: r, Status: status, Err: err}
    }(repo)
}
wg.Wait()
close(results)
```

No explicit queuing or scheduling — the OS scheduler handles fairness.

## Testing

- **Unit tests**: `*_test.go` files throughout (`cmd/`, `internal/`), testing individual functions
- **Integration tests**: focused `internal/workspace/*_test.go` files test each workspace operation, including `reset_test.go` for branch restoration and discard behavior
- **End-to-end tests**: `e2e/` invokes the compiled `gw` binary against isolated temporary homes and git fixtures
- **Run tests**: `just check` (tests + vet), `just e2e` (end-to-end)

Test fixtures often create temporary directories with real git repos, allowing tests to verify worktree creation and cleanup.

## Source Map

| File/Package | Purpose | Notes |
|---|---|---|
| `cmd/gw/main.go` | Entry point | Calls `cmd.Execute()` |
| `cmd/root.go` | Cobra setup | Registers all commands |
| `cmd/create.go` | Workspace creation | ~300 lines, handles interactive/flag modes |
| `cmd/delete.go` | Cleanup | Removes worktrees and branches |
| `cmd/status.go` | Status query | Calls `workspace.Status()` |
| `cmd/sync_cmd.go` | Rebase | Calls `workspace.Sync()` |
| `cmd/reset.go` | Workspace recovery | Calls `workspace.Service.Reset()`; supports `--discard` |
| `cmd/preset.go` | Preset management | CRUD for presets |
| `internal/workspace/service.go` | Core service boundary | Coordinates focused workspace operations |
| `internal/workspace/reset.go` | Workspace recovery | Restores recorded branches, skips dirty wanderers, then syncs |
| `internal/workspace/*_test.go` | Workspace tests | Focused tests by operation, including `reset_test.go` |
| `internal/state/state.go` | State persistence | ~200 lines, atomic writes |
| `internal/models/models.go` | Data structures | ~300 lines, JSON serialization |
| `internal/config/config.go` | Config loading | TOML parsing, legacy migration |
| `internal/discover/deepdiscover.go` | Repo discovery | Recursive scan, remote deduplication, and cache integration |
| `internal/gitops/gitops.go` | Git wrappers | ~600 lines, subprocess management |
| `internal/lifecycle/lifecycle.go` | Hook system | ~300 lines, placeholder expansion |
| `internal/plugin/` | Plugin management | Install, upgrade, remove |
| `docs/hooks.md` | Hook documentation | Comprehensive, with examples |
| `docs/plugins.md` | Plugin documentation | Installation, environment vars |
| `AGENTS.md` | Vendor-neutral agent guidance | Architecture and dev setup |
