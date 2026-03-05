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

## `gw run` TUI enhancements

- PTY allocation for subprocess output — some programs buffer stdout when not connected to a TTY (workaround: `stdbuf -oL` or tool-specific flags in `.grove.toml`)
- Log scrollback / search within a repo's log pane
- Aggregate view showing interleaved output from all repos
