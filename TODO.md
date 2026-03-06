# TODO

## Integration tests

Add end-to-end tests that exercise the main flows against real git repos (not mocks):
- `gw init` → `gw create` → `gw status` → `gw sync` → `gw delete`
- `gw create` → `gw add-repo` → `gw remove-repo`
- `gw create` → `gw rename` → `gw doctor`
- `gw create` → `gw run` (with actual processes, verify TUI launches)
- Lifecycle hooks fire in correct order with real `.grove.toml` files

## ~~Rethink init + repo discovery~~ (done)

Implemented in multi-dir init PR:
- Config stores `repo_dirs: list[Path]` instead of singular `repos_dir`
- `gw init` sets up `~/.grove`, optionally accepts dirs upfront
- `gw add-dir <path>` / `gw remove-dir <path>` to manage source directories
- `gw explore` scans all configured dirs recursively (up to 3 levels deep), prints discovered repos grouped by source dir, highlights new finds
- Discovery stays live — `create`/`add-repo` always scan fresh
- Backward compat: old config with `repos_dir` (singular) treated as single-element list

## Remove `status --all`

Deprecated in favor of `gw list -s`. Remove the `--all` flag from `gw status` after a few releases.

## ~~Smarter repo discovery — identity by git remote~~ (done)

Implemented: repos are now identified by `origin` remote URL instead of folder name.
- Interactive pickers (`create`, `add-repo`, `explore`) do deep scans and show `org/repo` from remote
- Same-named folders with different remotes are distinguished
- Same remote across multiple paths is deduped (direct children preferred)
- Non-interactive flags (`-r`) still use folder names for backward compat

## `gw run` TUI enhancements

- PTY allocation for subprocess output — some programs buffer stdout when not connected to a TTY (workaround: `stdbuf -oL` or tool-specific flags in `.grove.toml`)
- Log scrollback / search within a repo's log pane
- Aggregate view showing interleaved output from all repos
