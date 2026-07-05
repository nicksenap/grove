# Grove Documentation

**Grove** (`gw`) is a Git Worktree Workspace Orchestrator — a CLI tool that manages multi-repo worktree-based workspaces. It solves the problem of spinning up isolated feature branches across multiple repositories at once, with one command.

## The Problem Grove Solves

Without monorepos, developers must:
- Use `git worktree add` separately for each repo
- Track multiple branch names across different directories
- Jump between repos to run tests or builds
- Clean up worktrees one by one when done

Grove automates this workflow: **one command to create a workspace, all repos on the same branch, in a single directory.**

## Quick Start

### Install

```bash
brew install nicksenap/grove/grove
# or: go install github.com/nicksenap/grove/cmd/gw@latest

# Then add shell integration:
eval "$(gw shell-init)"
```

### First Commands

```bash
# Register your repo directories
gw init ~/dev ~/work/microservices

# Optionally: deep-scan to discover nested repos (2–3 levels)
gw explore

# Create a workspace for a feature branch (interactive)
gw create feat/login

# Or specify repos explicitly:
gw create my-feature -b feat/login -r svc-a,svc-b

# Navigate into the workspace
gw go my-feature

# Check status across all repos
gw status my-feature

# Run dev processes with a TUI
gw run my-feature

# Clean up when done
gw delete my-feature
```

## Key Concepts

### Workspace
A named collection of git worktrees, each from a different repo, all checked out to the same branch. A workspace directory contains one subdirectory per repo.

### Branch
All repos in a workspace check out the same branch name by default. You can override a repo's default base branch via its `.grove.toml` config.

### Preset
A named group of repos for quick workspace creation. Presets are defined in `~/.grove/config.toml`:

```bash
gw preset add backend -r svc-auth,svc-api,svc-worker
gw create my-feature -p backend  # uses the "backend" preset
```

### Hook
Automation hooks fire on workspace lifecycle events (create, delete, close). Global hooks live in `~/.grove/config.toml [hooks]`; per-repo hooks in `.grove.toml`.

### Plugin
Extend `gw` with custom commands (e.g., `gw-claude` for Claude Code integration, `gw-zellij` for terminal multiplexing).

## Configuration

### Global Config: `~/.grove/config.toml`

```toml
repo_dirs = [
  "~/dev",
  "~/work/microservices"
]
workspace_dir = "~/.grove/workspaces"

[presets]
backend = { repos = ["svc-auth", "svc-api"] }
frontend = { repos = ["web-app", "design-system"] }

[hooks]
post_create = "gw claude sync rehydrate {path}"
pre_delete = "gw claude sync harvest {path}"
```

### Per-Repo Config: `.grove.toml` (at repo root)

```toml
base_branch = "stage"  # override default branch (main/master)
setup = ["npm install", "npm run build"]  # run after worktree creation
```

## Common Commands

| Command | Purpose |
|---------|---------|
| `gw init <dirs>` | Register repo directories |
| `gw explore` | Deep-scan for nested repos |
| `gw create [NAME]` | Create a workspace (interactive or with `-b -r`/`-p`) |
| `gw list [-s]` | List workspaces (with `-s` shows git status) |
| `gw ws show <name>` | Show workspace details |
| `gw go <name>` | Change directory into workspace |
| `gw status <name>` | Git status across all repos |
| `gw sync <name>` | Rebase all repos onto base branch |
| `gw add-repo <name> -r <repo>` | Add a repo to existing workspace |
| `gw remove-repo <name> -r <repo>` | Remove a repo from workspace |
| `gw run <name>` | Run dev processes (interactive TUI) |
| `gw rename <name> --to <new>` | Rename a workspace |
| `gw delete <name>` | Clean up workspace (worktrees + branches) |
| `gw plugin install <repo>` | Install a plugin from GitHub |
| `gw wizard` | Interactive setup of plugins and hooks |
| `gw doctor` | Diagnose workspace issues |

## Architecture Overview

Grove is organized into clear layers:

- **Entry point**: `cmd/gw/main.go` → `cmd.Execute()` (Cobra)
- **Commands**: `cmd/*.go` handle user interaction and validation
- **Core logic**: `internal/workspace/` orchestrates git worktrees and manages workspace state
- **Configuration**: `internal/config/` loads global config; `internal/gitops/` reads per-repo `.grove.toml`
- **Data**: `internal/state/` persists workspace list to `~/.grove/state.json`
- **Integrations**: `internal/lifecycle/` runs hooks; `internal/plugin/` manages plugins; `internal/mcp/` serves workspace state to Claude Code

All repos are discovered once at command start via `internal/discover/`, then matched to the requested repos by name. Multi-repo operations use goroutines for concurrent execution.

## Next Steps

- **Understand the design**: Read [architecture.md](architecture.md)
- **Learn workflows**: Read [workflows.md](workflows.md)
- **Configure and integrate**: Read [operations.md](operations.md) and [integrations.md](integrations.md)

## Requirements

- **Go**: 1.25+ (to build from source)
- **Git**: Required on PATH
- **No external dependencies**: Single static binary with one `git` subprocess wrapper

## Key Source Files

- `cmd/gw/main.go` — Entry point
- `cmd/root.go` — Cobra setup and command registration
- `cmd/create.go` — Workspace creation
- `cmd/delete.go` — Cleanup
- `internal/workspace/workspace.go` — Core orchestration logic
- `internal/state/state.go` — State persistence
- `internal/models/models.go` — Data structures (Workspace, Preset, Hook, Config)
- `internal/config/config.go` — Config file loading
- `internal/discover/discover.go` — Repository discovery
- `internal/gitops/gitops.go` — Git subprocess wrappers
- `internal/lifecycle/lifecycle.go` — Lifecycle hooks
- `internal/plugin/` — Plugin management
- `internal/mcp/` — MCP server for Claude Code integration

## Testing

```bash
just check         # Run tests + vet
just build         # Build the gw binary
just e2e           # Run end-to-end tests
go test ./internal/workspace -v  # Test specific package
```

## Release Process

1. Add a new `## vX.Y.Z` section to `CHANGELOG.md`
2. Commit everything
3. Run `just release X.Y.Z` to create an annotated tag, push, and trigger the release workflow

## License

Grove is licensed under the MIT License (see [LICENSE](../LICENSE)).
