# TODO

## Integration tests

Add end-to-end tests that exercise the main flows against real git repos (not mocks):
- `gw init` → `gw create` → `gw status` → `gw sync` → `gw delete`
- `gw create` → `gw add-repo` → `gw remove-repo`
- `gw create` → `gw rename` → `gw doctor`
- `gw create` → `gw run` (with actual processes, verify TUI launches)
- Lifecycle hooks fire in correct order with real `.grove.toml` files

## Remove `status --all`

Deprecated in favor of `gw list -s`. Remove the `--all` flag from `gw status` after a few releases.

## Background tasks

`gw go --delete` already uses a detached subprocess for cleanup — the shell `cd`s immediately while deletion runs in the background. If cleanup fails, `gw doctor` catches stale state.

### Other background task candidates

- **`gw sync` fetch/rebase** — network I/O across multiple remotes is the slowest user-facing operation
- **`gw create` setup hooks** — `.grove.toml` `setup` commands (`npm install`, `uv sync`) can be slow; user could `cd` into the workspace immediately while setup runs in the background
- **`gw explore` deep scan** — recursive repo discovery could be slow on large filesystems; cache refresh could happen in background

### Design: keep it simple, avoid a global "background mode"

A global non-blocking config/env var would add `if background:` branches across every feature — too much surface area. Instead, start with a per-invocation flag:

- `gw create --background-setup` — cd into workspace immediately, run `.grove.toml` setup hooks in background
- Default stays synchronous (safe — user expects `npm install` to finish before they run `npm start`)
- If the flag sees enough use, promote to `.grove.toml` config: `setup_background = true`

This keeps the code simple: one flag, one branch, opt-in per invocation.

## `gw run` TUI enhancements

- PTY allocation for subprocess output — some programs buffer stdout when not connected to a TTY (workaround: `stdbuf -oL` or tool-specific flags in `.grove.toml`)
- Log scrollback / search within a repo's log pane
- Aggregate view showing interleaved output from all repos
