## What's New

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
