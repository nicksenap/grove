# Plugins

Grove supports external plugins that add commands. A plugin is a standalone executable named `gw-<name>`; when a built-in command does not match, `gw foo` resolves and executes `gw-foo`.

## Install methods

### From the registry

`gw` ships with a curated list of known plugins:

```bash
gw plugin search            # list all
gw plugin search editor     # filter by name, repo, or description
gw plugin install code      # bare name resolves through the registry
```

To add a plugin to the registry, open a PR against [`internal/plugin/registry.json`](../internal/plugin/registry.json). The list is only as current as the installed `gw`.

### From GitHub

Any repository that publishes `gw-<name>` release archives works, registry or not:

```bash
gw plugin install nicksenap/gw-run
gw plugin install nicksenap/gw-dispatch
gw plugin install nicksenap/gw-recipe
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
| `GROVE_STATE` | **Deprecated**, will be removed. Path to `state.json`, a private file; see [Reading Grove data](#reading-grove-data) |
| `GROVE_WORKSPACE` | Current workspace name, when cwd is inside one |

## Example plugins

### [gw-run](https://github.com/nicksenap/gw-run)

Runs per-repo `.grove.toml` `run` hooks across a workspace, prefixing output with `[repo]`.

```bash
gw plugin install nicksenap/gw-run
gw run
gw run my-feature
```

### [gw-dispatch](https://github.com/nicksenap/gw-dispatch)

Agent-agnostic plugin that creates a Grove workspace and starts a selected coding agent there with an initial prompt.

```bash
gw plugin install nicksenap/gw-dispatch
gw dispatch -n -r api,web -P "Implement login"
gw dispatch -b feat/login -p backend --agent pi -P "Implement login"
```

### [gw-recipe](https://github.com/nicksenap/gw-recipe)

Creates multi-repository workspaces from declarative YAML Recipes: pins repositories to refs, provisions worktrees through `gw create`, and runs a DAG of setup jobs. Formerly `gw recipe validate` and `gw create --recipe` in core.

```bash
gw plugin install nicksenap/gw-recipe
gw recipe validate stack.yaml
gw recipe create my-feature --branch feat/login --file stack.yaml
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

### Reading Grove data

Read workspace data through `gw` commands, not from files in `~/.grove`:

```bash
gw ws show -o json                      # the workspace containing the current directory
gw ws show "$GROVE_WORKSPACE" -o json   # the same, by name
gw list -o json                         # all workspaces
gw status NAME -o json                  # git status per repo
gw repos -o json                        # discovered repos
gw preset list --json                   # presets
```

The JSON these commands print is the supported interface: fields may be added, but existing fields are not renamed or removed. `-o name`, `-o path`, `-o tsv`, and `-o jsonl` are available on the same commands.

`state.json` is a private file. Its format can change in any release without notice, so do not read or write it. `GROVE_STATE` is deprecated for this reason: it is still set for now and will be removed in a later release. Change workspaces only through `gw` commands (`gw create`, `gw add-repo`, `gw delete`, ...).

If a plugin needs to store its own data, keep it in a file the plugin owns, for example `$GROVE_DIR/<plugin-name>.json`.
