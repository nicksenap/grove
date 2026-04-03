# Plugins

Grove supports external plugins that add new commands. Plugins are standalone executables named `gw-<name>` — when you run `gw foo`, Grove looks for a `gw-foo` binary and executes it.

## Install methods

### From GitHub

```bash
gw plugin install nicksenap/gw-dash
gw plugin install github.com/user/gw-something
```

This downloads the latest release binary for your OS/architecture from the repo's GitHub Releases. The naming convention follows [goreleaser](https://goreleaser.com/) defaults: `gw-dash_0.1.0_darwin_arm64.tar.gz`.

### Manual

Drop any executable named `gw-<name>` into `~/.grove/plugins/`:

```bash
cp my-plugin ~/.grove/plugins/gw-myplugin
chmod +x ~/.grove/plugins/gw-myplugin
```

Or place it anywhere on your `$PATH`.

## Managing plugins

```bash
gw plugin list                    # list installed plugins
gw plugin upgrade dash            # re-fetch latest release
gw plugin upgrade                 # upgrade all plugins
gw plugin remove dash             # uninstall
```

`upgrade` works for plugins installed via `gw plugin install` — it remembers the source repo. Manually installed plugins are skipped.

## How plugins work

When you run `gw <name>`, Grove first checks its built-in commands. If no match is found, it looks for `gw-<name>` in:

1. `~/.grove/plugins/`
2. `$PATH`

If found, Grove replaces its own process with the plugin (`exec`), passing these environment variables:

| Variable | Description |
|---|---|
| `GROVE_DIR` | Path to `~/.grove` |
| `GROVE_CONFIG` | Path to `config.toml` |
| `GROVE_STATE` | Path to `state.json` |
| `GROVE_WORKSPACE` | Current workspace name (if cwd is inside one) |

The plugin gets full control of the terminal — this means TUI plugins (like `gw-dash`) work seamlessly.

## Writing a plugin

A plugin can be any executable in any language. The simplest plugin is a shell script:

```bash
#!/bin/sh
# ~/.grove/plugins/gw-hello
echo "Hello! GROVE_DIR=$GROVE_DIR"
```

For distributable plugins, use Go with [goreleaser](https://goreleaser.com/) so that `gw plugin install` can find the right binary. See [gw-dash](https://github.com/nicksenap/gw-dash) for a reference implementation.

### Reading Grove state

Plugins read Grove's data files directly — no shared libraries or IPC:

- **`$GROVE_DIR/state.json`** — array of workspaces, each with name, path, branch, and repos
- **`$GROVE_DIR/config.toml`** — global config (repo_dirs, workspace_dir, presets)
- **`$GROVE_DIR/status/*.json`** — live agent state files (for dashboard-style plugins)
