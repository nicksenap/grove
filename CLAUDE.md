# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Grove?

Git Worktree Workspace Orchestrator — CLI tool invoked as `gw`. Manages multi-repo worktree-based workspaces so developers can spin up isolated branches across several repos at once.

## Development

- Go 1.25+
- Run `just check` for tests + vet
- Run `just build` to build the `gw` binary
- Run a single test: `go test ./internal/workspace -run TestName -v`
- Run e2e tests: `just e2e`

## Release Process

1. Commit everything
2. Tag + push: `just release X.Y.Z`
   - Creates annotated tag `vX.Y.Z`
   - Pushes tag to origin (triggers release workflow)
   - GoReleaser builds binaries and updates Homebrew tap automatically

## Per-repo config

Repos managed by Grove can have a `.grove.toml` at their root:
- `base_branch` — override the default branch for new worktrees (e.g. `stage`)
- `setup` — command(s) to run after worktree creation (string or list of strings)

## Architecture

Entry point: `main.go` → `cmd.Execute()` (Cobra).

### Package layout

- **cmd/** — Cobra commands and interactive pickers. Orchestrates user interaction.
- **internal/workspace/** — Core worktree orchestration (create, delete, status, sync). Uses goroutines for concurrent multi-repo operations.
- **internal/gitops/** — Thin wrappers around `git` subprocess calls. Includes `ReadGroveConfig()`.
- **internal/state/** — Workspace state persisted to `~/.grove/state.json`. Uses atomic writes.
- **internal/config/** — Global config from `~/.grove/config.toml`. Defines `GroveDir`, `ConfigPath`, `DefaultWorkspaceDir` constants.
- **internal/models/** — Data structs with JSON serialization.
- **internal/discover/** — Finds git repos in configured directories. Caches remote URLs on disk.
- **internal/claude/** — Syncs Claude Code memory directories between source repos and worktrees.
- **internal/console/** — Colored output helpers.
- **internal/update/** — Non-blocking version check.
- **internal/hook/** — Claude Code hook handler.
- **internal/plugin/** — Plugin install/upgrade/remove from GitHub releases.
- **internal/mcp/** — MCP JSON-RPC server for Claude Code.
- **internal/picker/** — Interactive terminal menus.
- **internal/stats/** — Workspace usage stats and heatmap.
- **internal/logging/** — Structured logging.
