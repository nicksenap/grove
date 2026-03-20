# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Grove?

Git Worktree Workspace Orchestrator — CLI tool invoked as `gw`. Manages multi-repo worktree-based workspaces so developers can spin up isolated branches across several repos at once.

## Development

- Python 3.12+, managed with `uv`
- Run `just check` for lint + format + tests
- Run `just dev` for editable install
- Run `just install` to install globally via uv tool
- Run a single test: `uv run pytest tests/test_workspace.py::test_name -v`
- Auto-fix lint: `just fix` / auto-format: `just fmt`
- Linter: ruff (line-length 100, rules: E, F, I, N, UP, B, SIM)

## Release Process

1. Bump version in `pyproject.toml`
2. Run `uv lock` to update the lockfile
3. Optionally write release notes in `release_notes.md` (root of repo)
4. Commit everything
5. Tag + push: `just release X.Y.Z`
   - Validates version in pyproject.toml matches
   - Creates annotated tag `vX.Y.Z`
   - Pushes tag to origin (triggers release workflow)
   - Workflow uses `release_notes.md` if present, otherwise auto-generates
   - Workflow auto-generates Homebrew formula with all Python resources

## Per-repo config

Repos managed by Grove can have a `.grove.toml` at their root:
- `base_branch` — override the default branch for new worktrees (e.g. `stage`)
- `setup` — command(s) to run after worktree creation (string or list of strings)

## Architecture

All source lives in `src/grove/`. Entry point: `gw` → `grove.cli:app` (Typer).

### Data flow

`cli.py` → `workspace.py` → `git.py` (subprocess) + `state.py` (JSON persistence) + `config.py` (TOML)

- **cli.py** — Typer commands and interactive pickers (simple-term-menu). Orchestrates user interaction.
- **workspace.py** — Core worktree orchestration (create, delete, status). Uses `_parallel()` for concurrent multi-repo operations via ThreadPoolExecutor.
- **git.py** — Thin wrappers around `git` subprocess calls. Raises `GitError` on failure. Includes `read_grove_config()` (LRU-cached — tests must clear it).
- **state.py** — Workspace state persisted to `~/.grove/state.json`. Uses atomic writes.
- **config.py** — Global config from `~/.grove/config.toml`. Defines `GROVE_DIR`, `CONFIG_PATH`, `DEFAULT_WORKSPACE_DIR` constants (patched in tests).
- **models.py** — Pure dataclasses: `Config`, `Workspace`, `RepoWorktree` with `to_dict`/`from_dict` serialization.
- **discover.py** — Finds git repos in configured directories. Caches remote URLs on disk (`~/.grove/cache/remotes.json`, 24h TTL).
- **tui.py** — Textual TUI for running workspace processes with sidebar + log pane.
- **claude.py** — Syncs Claude Code memory directories between source repos and worktrees.
- **console.py** — Rich output helpers (`success`, `error`, `info`, `warning`, `make_table`).
- **update.py** — Non-blocking version check (cached, background refresh).

### Testing patterns

- Tests use `tmp_grove` fixture (from `conftest.py`) which patches `GROVE_DIR`, `CONFIG_PATH`, `DEFAULT_WORKSPACE_DIR`, and `STATE_PATH` to temp directories.
- `fake_repos` fixture creates mock repo directories with `.git` dirs (not real git repos).
- The `read_grove_config` LRU cache is auto-cleared between tests via autouse fixture.
