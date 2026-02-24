# Grove

Git Worktree Workspace Orchestrator — CLI tool (`gw`).

## Development

- Python 3.12+, managed with `uv`
- Run `just check` for lint + format + tests
- Run `just dev` for editable install
- Run `just install` to install globally via uv tool

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

## Architecture

- `cli.py` — Typer commands and interactive pickers
- `config.py` — TOML config (~/.grove/config.toml)
- `state.py` — Workspace state JSON (~/.grove/state.json)
- `workspace.py` — Worktree orchestration
- `update.py` — Non-blocking version check (cached, background refresh)
- `console.py` — Rich output helpers
- `git.py` — Git subprocess calls
- `discover.py` — Repo discovery
- `models.py` — Dataclasses (Config, Workspace, RepoWorktree)
