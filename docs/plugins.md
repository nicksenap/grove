# Plugins

Grove supports external plugins that add commands. A plugin is a standalone executable named `gw-<name>`; when a built-in command does not match, `gw foo` resolves and executes `gw-foo`.

## Install methods

### From GitHub

```bash
gw plugin install nicksenap/gw-dispatch
gw plugin install igor-kupczynski/gw-code
```

Grove downloads the latest release binary for the current OS and architecture. Public releases require no token.

### Manual

Put an executable named `gw-<name>` in `~/.grove/plugins/` or on `$PATH`:

```bash
cp my-plugin ~/.grove/plugins/gw-myplugin
chmod +x ~/.grove/plugins/gw-myplugin
```

## Managing plugins

```bash
gw plugin list
gw plugin upgrade dispatch
gw plugin upgrade
gw plugin remove dispatch
```

`upgrade` works for plugins installed through `gw plugin install`; manually installed plugins are skipped.

## How plugins work

Grove checks built-in commands first, then searches:

1. `~/.grove/plugins/`
2. `$PATH`

The plugin receives control of the terminal and these environment variables:

| Variable | Description |
|---|---|
| `GROVE_DIR` | Path to `~/.grove` |
| `GROVE_CONFIG` | Path to `config.toml` |
| `GROVE_STATE` | Path to `state.json` |
| `GROVE_WORKSPACE` | Current workspace name, when cwd is inside one |

## Example plugins

### [gw-dispatch](https://github.com/nicksenap/gw-dispatch)

Agent-agnostic plugin that creates a Grove workspace and starts a selected coding agent there with an initial prompt.

```bash
gw plugin install nicksenap/gw-dispatch
gw dispatch -n -r api,web -P "Implement login"
gw dispatch -b feat/login -p backend --agent pi -P "Implement login"
```

### [gw-code](https://github.com/igor-kupczynski/gw-code)

Generates a multi-folder editor workspace for a Grove workspace and opens it in VS Code. Its configuration can select another compatible editor executable.

```bash
gw plugin install igor-kupczynski/gw-code
gw code my-workspace
gw code my-workspace --refresh
gw code my-workspace --path
```

## Writing a plugin

A plugin can be any executable in any language. The simplest plugin is a shell script:

```bash
#!/bin/sh
# ~/.grove/plugins/gw-hello
printf 'Hello from workspace %s\n' "${GROVE_WORKSPACE:-none}"
```

For released plugins, publish binaries using the conventional `gw-<name>` executable name and release archives supported by `gw plugin install`.

### Reading Grove state

Plugins can read `GROVE_STATE` for workspace data and `GROVE_CONFIG` for configuration. Treat these files as Grove-owned: use Grove commands for mutations rather than editing state directly.
