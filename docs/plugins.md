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

## First-party plugins

### gw-claude

Claude Code integration. Memory sync, session tracking, hook management.

- **Repo:** https://github.com/nicksenap/gw-claude
- **Install:** `gw plugin install nicksenap/gw-claude`

| Command | Description |
|---|---|
| `gw claude sync rehydrate <path>` | Copy memory from source repos to worktrees |
| `gw claude sync harvest <path>` | Copy newer memory from worktrees back to source |
| `gw claude sync migrate <old> <new>` | Rename/merge memory dir |
| `gw claude copy-md <path>` | Copy CLAUDE.md from source repo parent into workspace |
| `gw claude doctor` | Find orphaned memory directories |
| `gw claude hook install` | Register session tracking hooks in ~/.claude/settings.json |
| `gw claude hook uninstall` | Remove hooks |
| `gw claude hook status` | Check if hooks are installed |

Recommended hooks:

```toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
```

### gw-zellij

Zellij terminal multiplexer integration.

- **Repo:** https://github.com/nicksenap/gw-zellij
- **Install:** `gw plugin install nicksenap/gw-zellij`

| Command | Description |
|---|---|
| `gw zellij close-pane` | Close current Zellij pane |

Recommended hooks:

```toml
[hooks]
on_close = "gw zellij close-pane"
```

### gw-dash

Kanban-style TUI dashboard for monitoring Claude Code agents across workspaces.

- **Repo:** https://github.com/nicksenap/gw-dash
- **Install:** `gw plugin install nicksenap/gw-dash`
- **Usage:** `gw dash`

## Quick setup

```bash
gw wizard    # detects your tools, installs plugins, configures hooks
```

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
