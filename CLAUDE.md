# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## OpenWiki

This repository has documentation located in the /openwiki directory.

Start here:
- [OpenWiki quickstart](openwiki/quickstart.md)

OpenWiki includes repository overview, architecture notes, workflows, domain concepts, operations, integrations, testing guidance, and source maps.

When working in this repository, read the OpenWiki quickstart first, then follow its links to the relevant architecture, workflow, domain, operation, and testing notes.

## What is Grove?

Git Worktree Workspace Orchestrator — CLI tool invoked as `gw`. Manages multi-repo worktree-based workspaces so developers can spin up isolated branches across several repos at once.

## Development

- Go 1.25+
- Run `just check` for tests + vet
- Run `just build` to build the `gw` binary
- Run a single test: `go test ./internal/workspace -run TestName -v`
- Run e2e tests: `just e2e`

## Release Process

1. Add a new `## vX.Y.Z` section at the top of `CHANGELOG.md`
2. Commit everything
3. Tag + push: `just release X.Y.Z`
   - Creates annotated tag `vX.Y.Z`
   - Pushes tag to origin (triggers release workflow)
   - GoReleaser builds binaries, prepends changelog notes, and updates Homebrew tap

## Per-repo config

Repos managed by Grove can have a `.grove.toml` at their root:
- `base_branch` — override the default branch for new worktrees (e.g. `stage`)
- `setup` — command(s) to run after worktree creation (string or list of strings)

## Architecture

Entry point: `cmd/gw/main.go` → `cmd.Execute()` (Cobra).

Tool-specific integrations (Claude Code memory sync, Zellij, archive, dashboard) live in external plugins (`gw-claude`, `gw-zellij`, `gw-archive`, `gw-dash`) — Grove core is a pure git worktree orchestrator. Plugins hook into lifecycle events configured in `~/.grove/config.toml`.

### Package layout

- **cmd/** — Cobra commands and interactive pickers. Orchestrates user interaction.
- **cmd/gw/** — `main` package; thin entry point.
- **internal/config/** — Global config from `~/.grove/config.toml`. Defines `GroveDir`, `ConfigPath`, `DefaultWorkspaceDir` constants.
- **internal/console/** — Colored output helpers and table rendering.
- **internal/discover/** — Finds git repos in configured directories. Caches remote URLs on disk.
- **internal/gitops/** — Thin wrappers around `git` subprocess calls. Includes `ReadGroveConfig()`.
- **internal/lifecycle/** — Runs global lifecycle hooks (`post_create`, `pre_delete`, `on_close`) defined in `[hooks]`. Hooks may be bare command strings or tables with metadata (`stream`, `timeout`, `on_failure`); the global `--no-hooks`/`-n` flag skips them all. Plugins register here.
- **internal/logging/** — Structured logging.
- **internal/mcp/** — MCP JSON-RPC server exposing workspace state to Claude Code.
- **internal/models/** — Data structs with JSON serialization.
- **internal/picker/** — Interactive terminal menus.
- **internal/plugin/** — Plugin install/upgrade/remove from GitHub releases.
- **internal/state/** — Workspace state persisted to `~/.grove/state.json`. Uses atomic writes.
- **internal/stats/** — Workspace usage stats and heatmap.
- **internal/streamio/** — Per-line prefixing writer (e.g. `[post_create] ...`). Shared by `gw run` and the lifecycle hook paths for live/streamed output.
- **internal/update/** — Non-blocking version check.
- **internal/workspace/** — Core worktree orchestration (create, delete, status, sync). Uses goroutines for concurrent multi-repo operations.

<!-- OPENWIKI:START -->

## OpenWiki

This repository uses OpenWiki for recurring code documentation. Start with `openwiki/quickstart.md`, then follow its links to architecture, workflows, domain concepts, operations, integrations, testing guidance, and source maps.

The scheduled OpenWiki GitHub Actions workflow refreshes the repository wiki. Do not hand-edit generated OpenWiki pages unless explicitly asked; prefer updating source code/docs and letting OpenWiki regenerate.

<!-- OPENWIKI:END -->
