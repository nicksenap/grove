## What's New in v0.6.0

### Lifecycle Hooks

Repos can now define `teardown`, `pre_sync`, and `post_sync` hooks in `.grove.toml`, following an npm-style `pre_`/`post_` convention:

| Phase | Hook | When it runs |
|-------|------|-------------|
| Create | `setup` | After worktree creation |
| Delete | `teardown` | Before worktree removal |
| Sync | `pre_sync` | Before rebase (only when behind) |
| Sync | `post_sync` | After successful rebase |

Hook failures warn but never block the operation.

### New Commands

- **`gw add-repo`** — Add repos to an existing workspace (creates worktrees, runs setup hooks)
- **`gw remove-repo`** — Remove repos from a workspace (runs teardown hooks, cleans worktrees)
- **`gw rename`** — Rename a workspace (updates directory, state, and git worktree paths atomically)
- **`gw doctor`** — Diagnose workspace health (missing dirs, orphaned state, unregistered worktrees) with optional `--fix`
- **`gw run`** *(experimental)* — Run `run` hook commands across all repos in a workspace with live output

### Enhanced Status

- **`gw status --all`** — Overview of every workspace in a single table (branch, repo count, clean/modified summary)

### Performance

- `.grove.toml` reads are now cached per-process (`@functools.cache`), eliminating redundant TOML parses across commands
- Repo discovery uses stat checks instead of subprocess calls

### Bug Fixes

- Atomic rename — single state write prevents corruption on interruption
- Parallel execution guard against duplicate repo names
- `add-repo` no longer returns error exit code when all repos already present
- `gw run` no longer reads hook commands twice per repo
- Repo discovery no longer accepts git submodules as repositories
