---
type: "Reference"
title: "Workflows"
description: "Main Grove user workflows, from repository discovery and workspace creation through navigation, synchronization, execution, cleanup, and recovery."
tags: [grove, workflows, workspaces, git, cli]
---

# Workflows

This page explains the main user workflows and how they map to code.

## Workflow: Initial Setup

**Goal**: Register repo directories so Grove knows where to find projects.

### Steps

1. **Register repo directories**
   ```bash
   gw init ~/dev ~/work/microservices
   ```
   - Creates `~/.grove/config.toml` if it doesn't exist
   - Sets `repo_dirs = ["~/dev", "~/work/microservices"]`
   - Creates `~/.grove/state.json` (empty workspace list)

2. **Nested repositories are discovered automatically**
   - Configured directories are recursively scanned up to three levels deep
   - Hidden directories, `node_modules`, and `__pycache__` are skipped

3. **Add more directories later**
   ```bash
   gw add-dir ~/other/repos
   ```
   - Appends to `repo_dirs` in config

### Code Flow

- **`cmd/init_cmd.go`** — Calls `config.Save()` to persist directories
- **`internal/discover/deepdiscover.go`** — Recursively discovers repositories, deduplicates clones by remote URL, and caches remotes
- **`internal/config/config.go`** — `Save()` writes to TOML file; `Load()` reads and validates

**Key source**: `internal/discover/deepdiscover.go` scans configured directories for repositories.

---

## Workflow: Create a Workspace

**Goal**: Spin up a feature branch across multiple repos at once.

### Method 1: Interactive Selection (Recommended)

```bash
gw create feat-login
```

Prompts:
1. Select repos from presets (if any) or manually
2. Enter branch name (default: derived from workspace name)
3. Confirm and create

### Method 2: Flag-Based

```bash
gw create my-feature -b feat/login -r svc-a,svc-b
```

- `-b` — Branch name (required if not interactive)
- `-r` — Comma-separated repo names
- `-p` — Use a preset instead of `-r`

### Method 3: From a Preset

```bash
gw create -p backend
# or with explicit branch
gw create my-feature -p backend -b feat/login
```

### Special Cases: Branch Tracking and Source Provenance

The `create` command also supports:

- **`--track`** — Check out an existing remote branch instead of creating a new one (useful for pull request branches)
- **`--source-url`** — Record the URL a workspace was seeded from (e.g., GitHub PR, Notion page)
  ```bash
  gw create -b feat/login -r svc-a,svc-b \
    --source-url "https://github.com/org/repo/pull/42" \
    --source-provider github \
    --source-ref "42" \
    --source-title "Add login flow"
  ```
  - This metadata is stored in `state.json` for display and plugin use

### Code Flow

1. **`cmd/create.go`** — User input parsing
   - Loads config and discovers repos
   - Validates selections and branch name
   - Calls `workspace.Service.Create()` or `workspace.Service.CreateWithOpts()`

2. **`internal/workspace/workspace.go`** — Core creation logic
   - **`CreateWithOpts()`** — Main orchestrator
     - Checks for duplicate workspace (error if exists)
     - Resolves branch name (default: workspace name with `/` → `-`)
     - For each repo:
       - Calls `gitops.CreateWorktree()` to create the git worktree
       - Calls `gitops.Checkout()` to check out the branch (create or track)
       - Reads `.grove.toml` from repo and runs `setup` commands if present
     - Creates `Workspace` struct with all `RepoWorktree` entries
     - Calls `state.SaveWorkspace()` to persist

3. **`internal/gitops/gitops.go`** — Git operations
   - **`CreateWorktree()`** — Runs `git worktree add <path> <branch>`
     - Sets environment: `GIT_TERMINAL_PROMPT=0` (no interactive prompts)
     - Handles `GitError` and retries on transient failures
   - **`Checkout()`** — Runs `git checkout <branch>` or `git checkout -b <branch> origin/<branch>`
     - Tracks remote branch if it exists (with `--track`)
     - Creates new branch from base if not (`-b` flag)

4. **`internal/lifecycle/lifecycle.go`** — Post-create hook
   - If `post_create` hook configured, runs it with placeholders expanded:
     - `{name}` → workspace name
     - `{path}` → workspace directory path
     - `{branch}` → branch name
     - `{source_url}`, `{source_ref}`, `{source_title}` → from provenance
   - Respects hook metadata: `stream`, `timeout`, `on_failure`

5. **`internal/state/state.go`** — State persistence
   - Atomically writes workspace to `~/.grove/state.json`

### Key Decisions When Modifying

- **Branch naming**: Workspace name is derived from branch by replacing `/` with `-` (e.g., `feat/login` → `feat-login` workspace)
- **Base branch**: Default is `main` or `master` (auto-detected); override per-repo via `.grove.toml`
- **Worktree location**: All repos are checked out into `<WorkspaceDir>/<WorkspaceName>/<RepoName>/`
- **Setup commands**: Run sequentially in each repo after worktree creation; failure can be fatal (abort) or warning

---

## Workflow: Multi-Repo Operations

**Goal**: Query and modify the workspace across all repos.

### Check Status

```bash
gw status feat-login
```

- Runs `git status` in each repo concurrently
- Shows short status for each repo (new files, uncommitted changes, etc.)

### Rebase onto Base Branch

```bash
gw sync feat-login
```

- For each repo: runs `git rebase origin/<base-branch>`
- Useful after new commits are pushed to main/master

### Add a Repo to Existing Workspace

```bash
gw add-repo feat-login -r svc-c
```

- Creates a new worktree for `svc-c` in the same workspace directory
- Runs setup commands from `.grove.toml` if present
- Persists updated `Workspace` to state

### Remove a Repo from Workspace

```bash
gw remove-repo feat-login -r svc-a
```

- Removes the worktree for `svc-a` (calls `git worktree remove`)
- Removes the branch (calls `git branch -D`)
- Persists updated `Workspace` to state

### Code Flow

- **`cmd/status.go`** — Calls `workspace.Service.Status()` and displays results
- **`cmd/sync_cmd.go`** — Calls `workspace.Service.Sync()` for rebase
- **`cmd/addrepo.go`** / **`cmd/removerepo.go`** — Add/remove logic
- **`internal/workspace/workspace.go`**
  - `Status()` — Spawns goroutines for concurrent `git status` calls
  - `Sync()` — Spawns goroutines for concurrent `git rebase` calls
  - `AddRepo()` — Calls `CreateWorktree()` and persists updated workspace
  - `RemoveRepo()` — Calls `gitops.DeleteWorktree()` and persists update

### Key Decisions When Modifying

- **Concurrency**: `Status()` and `Sync()` use `sync.WaitGroup` to parallelize git operations; each repo is independent
- **Error handling**: Multi-repo operations continue even if one repo fails; errors are accumulated and reported
- **Destructive operations**: `RemoveRepo()` deletes the worktree and branch; consider making this optional

---

## Workflow: Run Dev Processes

**Goal**: Run per-repo commands (e.g., tests, builds) in a coordinated way.

### Interactive TUI

```bash
gw run feat-login
```

- Lists available commands from all repos' `.grove.toml`
- Opens a split-pane TUI (similar to tmux layout)
- Runs selected commands in parallel, shows output live

### Code Flow

- **`cmd/run.go`** — Calls `workspace.Service.Run()`
- **`internal/workspace/run.go`** — TUI implementation and command orchestration
  - Parses `[run]` sections from each repo's `.grove.toml`
  - Builds a list of available commands (e.g., "npm test", "npm run build")
  - Launches an interactive picker (type-to-search, multi-select)
  - For each selected command, spawns a goroutine to run it
  - Uses `internal/streamio/` to prefix each line with the repo name
  - Displays output live in the terminal

### Key Source Files

- `internal/workspace/run.go` — Main TUI and execution logic
- `internal/streamio/` — Per-line prefixing writer (e.g., `[svc-a] npm test output`)
- `.grove.toml` — Per-repo `[run]` section defines available commands

---

## Workflow: Cleanup

**Goal**: Delete a workspace (remove all worktrees and branches).

### Delete Command

```bash
gw delete feat-login
```

- Destructively removes the workspace without a confirmation prompt
- Pre-delete hook fires (for example, `./scripts/workspace-closing {path}` to save external state); configure `on_failure = "abort"` to enforce deletion policy
- For each repo in workspace:
  - Calls `git worktree remove --force <path>` to remove the worktree
  - Calls `git branch -D <branch>` to delete the branch
- Removes remaining Grove-owned workspace metadata and the workspace from state
- Optionally: runs `on_close` hook (e.g., `gw zellij close-pane`)

### Code Flow

1. **`cmd/delete.go`** — Runs `pre_delete` and orchestrates destructive deletion
2. **`internal/lifecycle/lifecycle.go`** — Fires `pre_delete` hook with `{path}` placeholder
3. **`internal/workspace/workspace.go`** — `Delete()` method
   - Calls `gitops.DeleteWorktree()` for each repo
   - Calls `state.DeleteWorkspace()` to remove from state
4. **`internal/operations/service.go`** — Applies the required `on_close` policy, then delegates hook execution to `internal/lifecycle/lifecycle.go`

### Key Decisions When Modifying

- **Hook timing**: `pre_delete` fires before worktree removal (still has access to working directories)
- **Deletion policy**: Deletion is destructive by default. A `pre_delete` hook with `on_failure = "abort"` is the extension point for safeguards.
- **Hook bypass**: `--no-hooks` skips lifecycle hooks, including any configured deletion policy

---

## Workflow: Preset Management

**Goal**: Save and reuse groups of repos for quick workspace creation.

### Create a Preset

```bash
gw preset add backend -r svc-auth,svc-api,svc-worker
```

- Adds or updates preset in `~/.grove/config.toml`
- Saved as `[presets.backend]` table

### List Presets

```bash
gw preset list
```

### Use a Preset

```bash
gw create my-feature -p backend
```

- Expands to the repos defined in the preset

### Code Flow

- **`cmd/preset.go`** — CRUD operations for presets
- **`internal/config/config.go`** — Loads/saves presets in TOML config
- **`cmd/create.go`** — Resolves presets when `-p` is used

---

## Workflow: Rename a Workspace

**Goal**: Rename an existing workspace without recreating it.

### Rename Command

```bash
gw rename feat-login --to login-v2
```

- Renames the workspace directory
- Updates all worktree paths in state
- Useful if you realize a better name later

### Code Flow

- **`cmd/rename.go`** — Parses the new name and calls `workspace.Service.Rename()`
- **`internal/workspace/workspace.go`** — `Rename()` method
  - Renames the workspace directory on disk
  - Updates the `Workspace.Name` and all `RepoWorktree.WorktreePath` entries
  - Persists updated state

---

## Workflow: Navigation and Shell Integration

**Goal**: Change directory into a workspace seamlessly.

### Change Directory

```bash
gw go feat-login
```

- Without shell integration: prints the path
- With shell integration (eval of `gw shell-init`): actually changes your shell's directory

### Code Flow

- **`cmd/go_cmd.go`** — Looks up workspace path and either:
  - Prints the path (if no shell integration)
  - Runs `cd` via a shell-specific function (bash/zsh: `__gw_go_impl`, nushell: `gw-go`)

### Shell Integration

```bash
eval "$(gw shell-init)"
```

- **`cmd/shellinit.go`** — Generates shell functions/aliases
  - Bash/Zsh: defines `gw()` wrapper function that calls `gw shell-init` on each command
  - Nushell: exports environment and defines aliases
  - Enables `gw go` to actually change directory in the current shell (not a subshell)

### Auto-CD After Create

With shell integration enabled, `gw create` automatically `cd`s into the new workspace (via `__gw_go_impl`).

---

## Workflow: Interactive Menus

**Goal**: Let users select from available options without flags.

### Implementation

Grove uses `internal/picker/` for terminal UI:

```bash
gw create  # No arguments — interactive
```

- Prompts for repo selection (with type-to-search)
- Prompts for branch name
- Confirms before creating

### Keyboard Shortcuts

- Type to search/filter
- Arrow keys to navigate
- Tab + arrow to multi-select (with `[all]` shortcut)
- Enter to confirm

### Code Flow

- **`internal/picker/picker.go`** — Terminal UI implementation
- **`cmd/*.go`** — Calls `picker.PickOne()` or `picker.PickMany()` when flags are missing

---

## Workflow: Diagnostics

**Goal**: Identify and resolve workspace health issues.

### Doctor Command

```bash
gw doctor
```

- Checks for common issues:
  - Orphaned worktrees (in state but missing on disk)
  - Missing git repos
  - Worktree conflicts
  - Config issues

- Suggests fixes

### Code Flow

- **`cmd/doctor.go`** — Runs diagnostics and reports issues
- Compares `~/.grove/state.json` against actual filesystem state

---

## Key Patterns When Modifying Workflows

### 1. **Always Validate Before Mutating State**
   - Check workspace exists before modifying
   - Verify repos are registered before creating
   - Confirm destructive operations

### 2. **Atomic State Writes**
   - Every workflow that modifies state should call `state.SaveWorkspace()` at the end
   - Failures should leave state intact

### 3. **Concurrent Git Operations**
   - Use `sync.WaitGroup` to parallelize independent operations
   - Collect errors in a slice; report all failures
   - Continue even if one repo fails (graceful degradation)

### 4. **Lifecycle Hooks Are Boundaries**
   - Fire hooks before/after operations
   - Hooks can abort operations (if `on_failure = "abort"`)
   - Always handle `lifecycle.ShouldAbort(err)` in commands

### 5. **Interactive vs. Flag-Based**
   - If flags provided, use them (fast path)
   - If no flags, prompt interactively (user-friendly)
   - Pickers should offer presets first (common case)

### 6. **Error Messages**
   - Be specific: which repo failed, what git command, why
   - Suggest next steps (e.g., "Check SSH keys" on auth failure)
   - Use `console.Errorf()` for user-facing errors
