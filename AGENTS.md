# AGENTS.md

This file provides vendor-neutral guidance to coding agents working in this repository.

## What is Grove?

Git Worktree Workspace Orchestrator — CLI tool invoked as `gw`. Manages multi-repo worktree-based workspaces so developers can spin up isolated branches across several repos at once.

## Development

- Go 1.26+ (see `go.mod`)
- Run `just check` before pushing — unit tests, e2e (uncached, `-count=1`), vet, gofmt, gocyclo, staticcheck, deadcode. CI and release run exactly this recipe; add new checks to the Justfile, not the workflows. The pre-commit hook runs only the fast subset (`just test vet fmt-check`).
- Lint tools are pinned in `go.mod` (`tool` directive) and run via `go tool`; bump with `go get -tool <pkg>@<version>`.
- `deadcode` fails on functions unreachable from `main`; test-only helpers belong in `_test.go` files.
- `skills/grove/` is the user-facing agent skill for driving `gw`. `cmd/skill_doc_test.go` fails if it names a command or flag that does not exist; when you add, rename, or remove a command or flag, update the skill and its `references/commands.md`.
- Run `just build` to build the `gw` binary
- Run a single test: `go test ./internal/workspace -run TestName -v`
- Run e2e tests: `just e2e` (Go tests in `e2e/`; set `GROVE_EXTERNAL_E2E=1` for the opt-in HTTPS clone)

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

Tool-specific integrations (coding agents, terminal multiplexers, archives, and dashboards) live in external plugins — Grove core is a pure git worktree orchestrator. Plugins hook into lifecycle events configured in `~/.grove/config.toml`.

### Package layout

- **cmd/** — Cobra commands and interactive pickers. Orchestrates user interaction.
- **cmd/gw/** — `main` package; thin entry point.
- **internal/config/** — Global config from `~/.grove/config.toml`. Defines `GroveDir`, `ConfigPath`, `DefaultWorkspaceDir` constants.
- **internal/console/** — Colored output helpers and table rendering.
- **internal/discover/** — Finds git repos in configured directories. Caches remote URLs on disk.
- **internal/gitops/** — Thin wrappers around `git` subprocess calls. Includes `ReadGroveConfig()`.
- **internal/lifecycle/** — Runs global lifecycle hooks (`resolve_branch`, `post_create`, `post_add_repo`, `pre_delete`, `on_close`) defined in `[hooks]`. `resolve_branch` is the only hook whose stdout Grove uses (it picks the branch for `gw create`). Hooks may be bare command strings or tables with metadata (`stream`, `timeout`, `on_failure`); the global `--no-hooks`/`-n` flag skips them all. Plugins register here.
- **internal/logging/** — Structured logging.
- **internal/models/** — Data structs with JSON serialization.
- **internal/operations/** — User-facing operation ordering (create, add-repo, delete): resolves branches, runs lifecycle hooks around the `workspace` primitives.
- **internal/output/** — Stable output formats shared by commands.
- **internal/picker/** — Interactive terminal menus.
- **internal/plugin/** — Plugin install/upgrade/remove from GitHub releases.
- **internal/state/** — Workspace state persisted to `~/.grove/state.json`. Uses atomic writes.
- **internal/stats/** — Workspace usage stats and heatmap.
- **internal/streamio/** — Per-line prefixing writer (e.g. `[post_create] ...`). Used by lifecycle hooks for live/streamed output.
- **internal/update/** — Non-blocking version check.
- **internal/workspace/** — Core worktree orchestration (create, delete, status, sync). Uses goroutines for concurrent multi-repo operations.

<!-- OPENWIKI:START -->

## OpenWiki

This repository has a generated `openwiki/` evidence index ([quickstart](openwiki/quickstart.md)). It is optional just-in-time context, not required startup reading.

- Treat source code and tests as authoritative. A brief's unknowns and review items are verification gaps, not automatic requirements.
- Prefer the narrowest quiet validation that proves the changed behavior. Preserve complete failure output.

No workflow refreshes the wiki; regenerate it manually with `openwiki code --update` and ship that as its own docs-only PR, never mixed into code changes. Do not hand-edit generated OpenWiki pages unless explicitly asked; update source code/docs and regenerate.

<!-- OPENWIKI:END -->
