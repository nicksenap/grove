# TODO

## `gw temp` — temporary workspaces for quick investigation

Spin up a disposable workspace for non-coding tasks (DLQ investigation, code review, log analysis). Key ideas:

- **Detached HEAD** — no branch creation, just checkout at `origin/main` tip after a parallel fetch
- **Skip setup hooks** — no `npm install` etc., we're just reading
- **Auto-cleanup** — delete workspace when done
- **Optional claude integration** — launch an interactive claude session in the temp workspace
- **Shell wrapper** handles cd + cleanup lifecycle (same `GROVE_CD_FILE` pattern as `gw create`)

### Open questions

- Should the prompt arg launch claude interactively (user can follow up) or non-interactively (`-p`, one-shot)?
  Interactive is probably better UX — `-p` exits immediately and auto-deletes before you can follow up.
- Zellij integration: `zellij action new-tab` doesn't support running commands, `zellij run` opens a pane not a tab.
  Simplest approach: just run everything in the current shell, no multiplexer awareness.
- Should `gw temp` reuse the `create_workspace` path or have its own lightweight `create_temp_workspace`?
  Own path is cleaner — skips branch checks, hook execution, and uses `git worktree add --detach`.

### Refactoring

- Move `shell/grove.sh` into the Python package and load via `importlib.resources` to eliminate the duplicated inline `_SHELL_FUNCTION` fallback in `cli.py`.

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

## Dashboard task modal UX

The create/edit modal works but the interaction is clunky:
- Preset picker (OptionList) and repo list (SelectionList) are inline but navigating between them isn't smooth
- `/` search only activates when repo list is focused — not discoverable
- Consider a custom widget that combines preset selection + repo toggling in one cohesive interaction
- Investigate Textual's `on_key` override for modal-local keybindings that don't conflict with the app's `priority=True` enter binding

## `gw run` TUI enhancements

- PTY allocation for subprocess output — some programs buffer stdout when not connected to a TTY (workaround: `stdbuf -oL` or tool-specific flags in `.grove.toml`)
- Log scrollback / search within a repo's log pane
- Aggregate view showing interleaved output from all repos
