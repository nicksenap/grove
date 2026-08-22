---
type: "Reference"
title: "Operations"
description: "Operational guidance for installing, configuring, maintaining, troubleshooting, and validating Grove workspaces and its local runtime state."
tags: [grove, operations, configuration, troubleshooting, maintenance]
---

# Operations

This page covers configuration, maintenance, troubleshooting, and runtime operations.

## Installation & Setup

### Install Methods

#### Homebrew (Recommended)
```bash
brew install nicksenap/grove/grove
```

#### Go Install
```bash
go install github.com/nicksenap/grove/cmd/gw@latest
```

#### From Source
```bash
git clone https://github.com/nicksenap/grove.git
cd grove && go build -o gw ./cmd/gw
mv gw /usr/local/bin/
```

#### Upgrading
```bash
brew update && brew upgrade grove
```

### Shell Integration

**Required** for `gw go` to change your working directory in the current shell.

**Bash / Zsh** — Add to `.zshrc` or `.bashrc`:
```bash
eval "$(gw shell-init)"
```

**Nushell** — Generate and source init file:
```nushell
gw shell-init --shell nu | save -f ~/.config/nushell/grove.nu
# then add to config.nu:
source grove.nu
```

**Effect**: After shell init, `gw go <workspace>` changes your directory and `gw create` auto-cds into new workspaces.

---

## Configuration

### Global Config: `~/.grove/config.toml`

Automatically created on first `gw init`. Example:

```toml
repo_dirs = [
  "~/dev",
  "~/work/microservices"
]
workspace_dir = "~/.grove/workspaces"

[presets]
backend = { repos = ["svc-auth", "svc-api", "svc-worker"] }
frontend = { repos = ["web-app", "design-system"] }

[hooks]
post_create = "./scripts/workspace-created {path}"
pre_delete = "./scripts/workspace-closing {path}"
on_close = "gw zellij close-pane"

[hooks.post_create]
command     = "npm install && npm run build"
description = "Install deps and build assets"
stream      = true
timeout     = "5m"
on_failure  = "abort"
```

#### Fields

| Field | Type | Purpose |
|-------|------|---------|
| `repo_dirs` | `[string]` | Directories to scan for git repos (scans one level deep) |
| `workspace_dir` | `string` | Where Grove creates workspace directories (default: `~/.grove/workspaces`) |
| `[presets]` | `[table]` | Named repo groups for quick creation (e.g., `backend = { repos = [...] }`) |
| `[hooks]` | `[table]` | Lifecycle hooks (post_create, pre_delete, on_close) |

#### Config Locations
- **Config file**: `~/.grove/config.toml`
- **State file**: `~/.grove/state.json`
- **Plugin directory**: `~/.grove/plugins/`
- **Workspace directories**: `~/.grove/workspaces/<name>/` (default)

#### Legacy Migration
Grove automatically migrates old config format:
- **Old**: `repos_dir = "~/dev"` (single directory)
- **New**: `repo_dirs = ["~/dev"]` (list of directories)

When loaded, old format is converted and config is re-saved.

---

### Per-Repo Config: `.grove.toml`

Optional file at repository root. Controls repo-specific behavior:

```toml
# Override the default branch (main/master) for new worktrees
base_branch = "stage"

# Commands to run after worktree creation
setup = [
  "npm install",
  "npm run build"
]

# Commands available in `gw run` interactive menu
[run]
test = "npm test"
build = "npm run build"
lint = "npm run lint"
dev = "npm run dev"
```

#### Fields

| Field | Type | Purpose |
|-------|------|---------|
| `base_branch` | `string` | Default branch for new worktrees (e.g., `stage` instead of `main`) |
| `setup` | `string` or `[string]` | Command(s) to run after worktree creation |
| `[run]` | `[table]` | Available commands in `gw run` TUI (name → command) |

#### Setup Commands
- Run sequentially after worktree is created
- Work in the repo's worktree directory
- Can be string (single command) or list (multiple commands)
- Failures abort unless `on_failure = "warn"` (see hooks below)

---

## Lifecycle Hooks

Hooks automate actions at key workspace lifecycle moments. There are two levels:

### Global Hooks (Workspace-Level)

Defined in `~/.grove/config.toml [hooks]`. Fire on all workspaces.

#### Hook Types

| Hook | Fired By | When | Typical Use |
|------|----------|------|------------|
| `post_create` | `gw create` | After workspace creation, before returning to user | Prepare agent context, check in with dashboard |
| `pre_delete` | `gw delete` | Before worktree removal (still has access to files) | Export work, harvest state, notify external systems |
| `on_close` | `gw go -c` | When closing a workspace's terminal pane | Close tmux/Zellij pane |

#### Placeholders

Hooks can include placeholders that Grove expands before execution:

| Placeholder | Value |
|---|---|
| `{name}` | Workspace name (e.g., `my-feature`) |
| `{path}` | Absolute path to workspace directory (e.g., `~/.grove/workspaces/my-feature`) |
| `{branch}` | Branch name (e.g., `feat/login`) |
| `{source_url}` | Original source URL (e.g., GitHub PR) — empty if not provided |
| `{source_ref}` | Provider-specific ref (e.g., PR number) — empty if not provided |
| `{source_title}` | Human-readable title from source — empty if not provided |

**Injection Prevention**: Placeholders are single-quoted by Grove to prevent shell injection. A branch named `feat/x; rm -rf ~` becomes `'feat/x; rm -rf ~'`.

#### Hook Syntax: String vs. Table

**Simple string** (quiet execution):
```toml
[hooks]
post_create = "./scripts/workspace-created {path}"
```

**Table with metadata** (advanced control):
```toml
[hooks.post_create]
command     = "npm install && npm run build"
description = "Install deps and build assets"
stream      = true
timeout     = "5m"
on_failure  = "abort"
```

#### Hook Metadata

| Field | Default | Meaning |
|-------|---------|---------|
| `command` | — | The shell command to run (required in table form) |
| `description` | — | What the hook does (documents the hook inline) |
| `stream` | `false` | Show output live in terminal (each line prefixed with hook name) |
| `timeout` | none | Maximum duration (Go duration format: `30s`, `5m`, `1h`). Hook is killed if exceeded. |
| `on_failure` | `warn` | How to handle failure: `warn` (log and continue) or `abort` (fatal) |

#### Output Handling

**When `stream = false` (default)**:
- Hook output is captured silently
- On success: nothing is printed
- On failure: captured output is echoed (prefixed) so you see what went wrong
- Example: `[post_create] npm ERR! Could not find module xyz`

**When `stream = true`**:
- Hook output streams live to the terminal as it runs
- Each line is prefixed (e.g., `[post_create] added 412 packages`)
- Use this for long-running hooks (builds, downloads) so the terminal doesn't look hung
- Example: `[post_create] npm warn ...`, then `[post_create] added 412 packages`

#### Global Hook Disabling

Skip all hooks for a single command:
```bash
gw create my-feature --no-hooks
gw delete my-feature -n  # short form
```

Or disable all hooks permanently (not recommended):
```bash
# There's no global config option; use --no-hooks per command
```

### Per-Repo Hooks

Defined in each repo's `.grove.toml [setup]` section.

- Run **sequentially** after `git worktree add` completes
- Run in the repo's worktree directory
- Can be string or list of strings
- Example: Install deps, compile, generate code

```toml
# Single command
setup = "npm install"

# Multiple commands
setup = [
  "npm install",
  "npm run build",
  "npm run generate-types"
]
```

---

## Plugins

Plugins extend Grove with custom commands. They're standalone binaries prefixed with `gw-`.

### Install Plugins

#### From GitHub
```bash
gw plugin install nicksenap/gw-dash
gw plugin install github.com/nicksenap/gw-zellij
```

Downloads the latest release binary for your OS/arch from GitHub Releases. Expects naming convention: `gw-<name>_<version>_<os>_<arch>.tar.gz` (goreleaser standard).

#### Manual Installation
Drop any executable named `gw-<name>` into `~/.grove/plugins/`:
```bash
cp my-plugin ~/.grove/plugins/gw-myplugin
chmod +x ~/.grove/plugins/gw-myplugin
```

Or place it anywhere on `$PATH`.

### Manage Plugins

```bash
gw plugin list                    # List installed plugins
gw plugin upgrade dash            # Re-fetch latest release for dash
gw plugin upgrade                 # Upgrade all plugins
gw plugin remove dash             # Uninstall a plugin
```

### How Plugins Work

When you run `gw foo`:
1. Grove checks its built-in commands
2. If not found, looks for `gw-foo` in:
   - `~/.grove/plugins/`
   - `$PATH`
3. If found, executes the plugin with these environment variables:

| Variable | Value |
|----------|-------|
| `GROVE_DIR` | Path to `~/.grove` |
| `GROVE_CONFIG` | Path to `config.toml` |
| `GROVE_STATE` | Path to `state.json` |
| `GROVE_WORKSPACE` | Current workspace name (if cwd is inside one); empty otherwise |

The plugin gets full control of the terminal (no output capture). This enables TUI plugins (dashboards, pickers) to work seamlessly.

### Plugin Lifecycle Hooks

Plugins can be invoked by lifecycle hooks. Install the plugin using its real GitHub `OWNER/REPOSITORY` identifier, then add its recommended hook commands to `config.toml`. Grove fires them normally.

### First-Party Plugins

Grove maintains reference plugins. Agent-specific memory or session integrations can use the same external plugin contract without adding vendor-specific behavior to core.

#### `gw-zellij`
Zellij terminal integration. Auto-create panes, close-pane commands.
```bash
gw plugin install nicksenap/gw-zellij
```

#### `gw-dash`
Agent monitoring dashboard. Real-time workspace status.
```bash
gw plugin install nicksenap/gw-dash
```

#### `gw-archive`
Archive workspaces for later replay.
```bash
gw plugin install nicksenap/gw-archive
```

---

## State Management

### State File: `~/.grove/state.json`

Persists the list of workspaces and their configuration:

```json
{
  "workspaces": [
    {
      "name": "feat-login",
      "path": "~/.grove/workspaces/feat-login",
      "branch": "feat/login",
      "created_at": "2024-01-15T10:30:45.123456",
      "repos": [
        {
          "repo_name": "svc-api",
          "source_repo": "~/dev/svc-api",
          "worktree_path": "~/.grove/workspaces/feat-login/svc-api",
          "branch": "feat/login"
        },
        {
          "repo_name": "svc-auth",
          "source_repo": "~/dev/svc-auth",
          "worktree_path": "~/.grove/workspaces/feat-login/svc-auth",
          "branch": "feat/login"
        }
      ],
      "source": {
        "provider": "github",
        "url": "https://github.com/org/repo/pull/42",
        "ref": "42",
        "title": "Add login flow"
      }
    }
  ]
}
```

#### Fields

- **name** — Workspace identifier
- **path** — Directory containing all worktrees
- **branch** — Common branch for all repos in workspace
- **created_at** — Timestamp (ISO 8601)
- **repos** — List of repos in workspace with worktree details
- **source** — Optional: where workspace was seeded from (e.g., GitHub PR, Notion page)

#### Atomic Writes

State is written atomically to prevent corruption:
1. Write to temporary file
2. Fsync to disk
3. Atomic rename to `state.json`

If Grove crashes mid-operation, state is left unchanged.

---

## Troubleshooting

### `gw doctor`

Diagnose workspace health issues:

```bash
gw doctor
```

Checks for:
- Orphaned worktrees (in state but missing on disk)
- Missing git repos (in state but source repo deleted)
- Worktree conflicts (path collisions)
- Config issues (missing repo_dirs, invalid presets)

Suggests fixes for each issue.

### Common Issues

#### "No repo directories configured"
```bash
gw init ~/dev ~/work
```

#### "Preset not found"
```bash
gw preset list  # Check available presets
gw create -p <name>  # Use correct name
```

#### "Workspace already exists"
Either rename the workspace:
```bash
gw rename old-name --to new-name
```

Or delete and recreate:
```bash
gw delete old-name
gw create old-name ...
```

#### "Git SSH key issue"
Grove sets `GIT_TERMINAL_PROMPT=0` to disable interactive prompts. If auth fails:
1. Ensure SSH key is loaded: `ssh-add ~/.ssh/id_ed25519`
2. Test connectivity: `ssh -T git@github.com`
3. Configure Git to use specific key (if needed): `git config core.sshCommand "ssh -i ~/.ssh/custom-key"`

#### "Worktree path exists"
Grove cleans up worktrees on delete, but manual deletion of files can leave state inconsistent. Run:
```bash
gw doctor         # Identifies orphaned entries
gw delete <name>  # Destructively removes the registered workspace
```

#### "Hook timeout"
Long-running setup commands can exceed default timeout. Configure:

```toml
[hooks.post_create]
command = "npm install && npm run build"
timeout = "10m"  # Increase timeout
```

Or move long commands to per-repo setup:

```toml
# In .grove.toml
setup = ["npm install", "npm run build"]
```

(Per-repo setup runs sequentially and has no timeout.)

---

## Development & Testing

### Build

```bash
just build
# or: go build -o gw ./cmd/gw
```

### Test

```bash
just check
# or: go test ./...
```

Test a single package:
```bash
go test ./internal/workspace -v
```

Test a single test function:
```bash
go test ./internal/workspace -run TestCreateWorkspace -v
```

### End-to-End Tests

```bash
just e2e
```

Creates temporary directories with real git repos and exercises `gw` commands.

### Code Coverage

```bash
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## Release Process

1. **Update changelog**: Add a new `## vX.Y.Z` section at the top of `CHANGELOG.md` with release notes
2. **Commit**: `git commit -am "Prepare vX.Y.Z"`
3. **Tag and release**: `just release X.Y.Z`
   - Creates annotated tag `vX.Y.Z`
   - Pushes tag to origin
   - GitHub Actions workflow builds binaries and updates Homebrew tap

---

## Performance

### Multi-Repo Operations

Grove parallelizes independent operations using goroutines:

```bash
gw status feat-login  # Runs git status in all repos concurrently
gw sync feat-login    # Runs git rebase in all repos concurrently
```

Expected times for typical workspaces (5–10 repos):
- `gw create` — 2–5 seconds (git clone, worktree setup)
- `gw status` — 1–2 seconds (git status calls)
- `gw delete` — <1 second (cleanup)

### Repo Discovery

Configured repository directories are recursively scanned up to three levels deep. Hidden directories, `node_modules`, and `__pycache__` are skipped.

### Caching

Grove caches resolved remote URLs under `~/.grove/cache/`, while filesystem discovery runs for each command. Repositories that share a remote are deduplicated, with a direct child of a configured directory preferred over a nested clone.

---

## Monitoring & Logging

### Verbose Logging

Enable debug logging for a command:

```bash
gw --verbose create my-feature
gw -v status my-feature
```

Outputs detailed logs to stderr.

### Stats & Usage

View workspace creation history and heatmap:

```bash
gw stats
```

Shows:
- Recent workspace creation dates
- Heatmap of workspace activity
- Repository usage frequency

---

## Security

### No Credentials Storage

Grove does **not** store credentials or SSH keys. It relies on:
- `$HOME/.ssh/` for SSH keys
- `git config` for credential helpers
- SSH agent for key management

### No External Calls

Grove makes no network requests except to GitHub (for plugin downloads) and your own git remotes. No telemetry, no analytics.

### Sensitive Data

Do **not** include credentials or tokens in:
- Hook commands (they're in plaintext in `config.toml`)
- `.grove.toml` files
- Workspace names or branch names

Use environment variables or credential managers instead.

---

## Next Steps

- Review [workflows.md](workflows.md) for common usage patterns
- Read [integrations.md](integrations.md) for plugin/AI tool setup
