# Changelog

## v1.1.0

### Plugin architecture — Grove is now tool-agnostic

Claude Code and Zellij integrations have been extracted from core into standalone plugins. Grove's core is now a pure git worktree orchestrator; tool-specific behavior is composable via lifecycle hooks.

**Install plugins:**

```bash
gw plugin install nicksenap/gw-claude   # Claude Code memory sync + session tracking
gw plugin install nicksenap/gw-zellij   # Zellij close-pane
```

**Or run the new wizard:**

```bash
gw wizard   # detects your tools, installs plugins, configures hooks
```

### New: `pre_delete` lifecycle hook

Fires before workspace teardown. Used by `gw-claude` to harvest memory back to source repos before worktrees are destroyed.

```toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
on_close = "gw zellij close-pane"
```

### New: `gw wizard`

Interactive setup that detects your environment (Claude Code, Zellij) and offers to install the right plugins and configure hooks. Run it after `gw init` or after upgrading.

### Breaking changes

- `gw hook install/uninstall/status` removed — use `gw claude hook install` (from the plugin) instead
- `gw _hook` hidden command removed — the plugin handles this now
- `claude_memory_sync` config field removed — memory sync is now opt-in via hooks
- Legacy Zellij fallback removed — configure `[hooks] on_close` instead
- Legacy `CLAUDE.md` copy fallback removed — use `post_create` hook instead

### Migration from v1.0.x

```bash
gw hook uninstall                        # remove old core hooks (if installed)
gw plugin install nicksenap/gw-claude    # install the plugin
gw wizard                                # configure everything interactively
```

## v1.0.4

### `gw ws delete` — interactive delete under the `ws` subcommand

`gw ws delete` now works the same as `gw delete` — with interactive multi-select when no name is given, `--force` flag, and tab completion. Consistent UX across both entry points.

### Global lifecycle hooks

New `[hooks]` section in `~/.grove/config.toml` lets you integrate Grove with any terminal multiplexer — not just Zellij.

```toml
[hooks]
on_close = "zellij action close-pane"
```

`gw go -c` now fires the `on_close` hook instead of hardcoding Zellij. Placeholders `{name}`, `{path}`, `{branch}` are expanded with shell quoting to prevent injection.

Existing Zellij users: everything keeps working via a fallback. Run `gw doctor` to see the migration hint.

### Doctor checks for missing hooks

`gw doctor` now flags when you're running inside Zellij without an `on_close` hook configured, with a suggested action to add one.
