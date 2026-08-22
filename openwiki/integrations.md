---
type: "Reference"
title: "Integrations"
description: "Grove integration points, including external plugins, lifecycle hooks, Claude Code support, and Zellij workflows."
tags: [grove, integrations, plugins, hooks]
---

# Integrations

This page covers external integrations, including plugins and AI tools.

## Plugin Ecosystem Overview

Grove's core is a pure git worktree orchestrator. All tool-specific integrations (Claude Code, Zellij, etc.) live in **external plugins** that hook into workspace lifecycle events.

### Why Plugins?

Keeping tool logic out of Grove core makes it:
- **Lightweight** — Single static binary with no dependencies
- **Extensible** — Anyone can write plugins without modifying Grove
- **Composable** — Mix and match plugins (Claude + Zellij + Dash)

Plugins are invoked by lifecycle hooks configured in `~/.grove/config.toml`.

---

## First-Party Plugins

Grove maintains reference implementations demonstrating the plugin model.

### `gw-claude` — Claude Code Integration

Full integration with Claude Code for memory sync, session tracking, and .md file management.

#### Install

```bash
gw plugin install nicksenap/gw-claude
```

#### Features

- **Memory sync** — Claude Code memory persists across workspaces
  - `gw claude sync rehydrate {path}` — Load memory into new workspace
  - `gw claude sync harvest {path}` — Save memory from workspace before deletion
  - Memory is stored in `~/.grove/.claude-memory/` (per-workspace JSON)

- **CLAUDE.md copy** — Project instructions automatically copied to new workspaces
  - Looks for `CLAUDE.md` in source repo root
  - Copies to workspace on creation
  - Ensures Claude Code agents have consistent context

- **Session tracking** — Records workspace lifecycle for the dashboard
  - Logs creation, deletion, and modifications
  - Enables `gw-dash` to show workspace history

#### Usage

Lifecycle hook configuration:

```toml
# ~/.grove/config.toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
```

#### How It Works

**On workspace creation** (`post_create`):
1. `gw claude sync rehydrate {path}` — Loads memory from the source repo into the workspace
2. `gw claude copy-md {path}` — Copies `CLAUDE.md` from source to workspace

**On workspace deletion** (`pre_delete`):
1. `gw claude sync harvest {path}` — Saves any Claude Code updates back to source repo memory

**Memory storage**:
- Location: `~/.grove/.claude-memory/<repo-name>.json`
- Format: JSON with Claude Code session metadata
- Thread-safe: Atomic writes like state.json

#### Commands

```bash
gw claude sync rehydrate <path>    # Load memory into workspace
gw claude sync harvest <path>      # Save memory from workspace
gw claude copy-md <path>           # Copy CLAUDE.md to workspace
gw claude hook install             # Register default hooks
gw claude <subcommand> -h          # Help for subcommands
```

---

### `gw-zellij` — Terminal Multiplexer Integration

Integrates with Zellij terminal multiplexer for automatic workspace pane management.

#### Install

```bash
gw plugin install nicksenap/gw-zellij
```

#### Features

- **Auto pane creation** — Create Zellij pane on workspace creation
- **Pane closing** — Close pane when navigating away from workspace
- **Layout synchronization** — Keep Zellij layout in sync with workspace state

#### Usage

Hook configuration:

```toml
# ~/.grove/config.toml
[hooks]
post_create = "gw zellij create-pane {name}"
on_close = "gw zellij close-pane"
```

#### Commands

```bash
gw zellij create-pane <name>   # Create a new Zellij pane
gw zellij close-pane           # Close current Zellij pane
gw zellij <subcommand> -h      # Help for subcommands
```

---

### `gw-dash` — Agent Dashboard

Kanban-style TUI for monitoring Claude Code agents across workspaces.

#### Install

```bash
gw plugin install nicksenap/gw-dash
gw dash
```

#### Features

- **Workspace kanban** — Shows workspace status in columns (pending, active, completed)
- **Session tracking** — Displays Claude Code session metadata
- **Live updates** — Refreshes as workspaces are created/deleted
- **Zellij integration** — Optional: Zellij window management

#### Usage

```bash
gw dash
```

Opens a full-screen TUI with:
- Left sidebar: Workspace list
- Main area: Kanban columns
- Status bar: Commands and help

#### Keybindings

See [gw-dash README](https://github.com/nicksenap/gw-dash) for full keybindings.

#### Architecture

- Reads from `~/.grove/state.json` and Claude Code session tracking files
- No polling — subscribes to file change events
- Lightweight: single-threaded event loop

---

### `gw-archive` — Workspace Archival

Archive workspaces for later replay or audit.

#### Install

```bash
gw plugin install nicksenap/gw-archive
```

#### Features

- **Snapshot workspaces** — Export full worktree state to archive
- **Replay workspaces** — Restore from archive
- **Audit trail** — Records workspace changes over time

#### Commands

```bash
gw archive export <name> <file>   # Export workspace to tar.gz
gw archive import <file>           # Restore workspace from archive
gw archive list                    # List archived workspaces
```

---

## Workspace Source Provenance

When creating a workspace from an external source (GitHub PR, Notion page, Slack thread), you can record the source URL and metadata:

```bash
gw create my-feature -b feat/login -r svc-a,svc-b \
  --source-url "https://github.com/org/repo/pull/42" \
  --source-provider github \
  --source-ref "42" \
  --source-title "Add login flow"
```

This metadata is:
- Stored in workspace state (`.source` field)
- Passed to hooks via placeholders: `{source_url}`, `{source_ref}`, `{source_title}`
- Available to plugins (for example Claude memory and dashboard integrations)

**Use cases**:
- Claude Code agents → Trace back to original PR or task
- Dashboard → Show workspace origin
- Automation → Route workspaces based on source provider

---

## Writing Custom Plugins

Any executable named `gw-<name>` can be a plugin. Grove will:
1. Check for built-in command
2. Look in `~/.grove/plugins/` and `$PATH`
3. Exec the plugin with environment variables

### Plugin Template (Bash)

```bash
#!/bin/bash
# gw-mycommand — A simple Grove plugin

WORKSPACE="${GROVE_WORKSPACE:-unknown}"
CONFIG="${GROVE_CONFIG}"
STATE="${GROVE_STATE}"

case "${1:-}" in
  "--help")
    echo "gw mycommand — My custom command"
    echo "Usage: gw mycommand [options]"
    ;;
  *)
    echo "Running in workspace: $WORKSPACE"
    echo "Config: $CONFIG"
    echo "State: $STATE"
    ;;
esac
```

### Plugin Template (Go)

```go
package main

import (
    "fmt"
    "os"
)

func main() {
    workspace := os.Getenv("GROVE_WORKSPACE")
    config := os.Getenv("GROVE_CONFIG")
    state := os.Getenv("GROVE_STATE")
    
    fmt.Printf("Running in workspace: %s\n", workspace)
    fmt.Printf("Config: %s\n", config)
    fmt.Printf("State: %s\n", state)
}
```

### Register as Lifecycle Hook

To make your plugin respond to workspace lifecycle events:

1. Write commands that accept `{path}`, `{name}`, `{branch}` placeholders
2. Add to `~/.grove/config.toml`:

```toml
[hooks]
post_create = "gw mycommand post-create {path} {name} {branch}"
pre_delete = "gw mycommand pre-delete {path} {name}"
```

3. Grove fires your hook on events

### Environment Variables Available

| Variable | Value | Example |
|----------|-------|---------|
| `GROVE_DIR` | Grove config directory | `~/.grove` |
| `GROVE_CONFIG` | Path to config.toml | `~/.grove/config.toml` |
| `GROVE_STATE` | Path to state.json | `~/.grove/state.json` |
| `GROVE_WORKSPACE` | Current workspace (if in one) | `feat-login` |

You can parse `GROVE_CONFIG` (TOML) and `GROVE_STATE` (JSON) to access full Grove state.

---

## Extensibility Patterns

### Pattern 1: Lifecycle Hook Handler

Listen to workspace events and react:

```bash
# In plugin
case "$1" in
  "post-create")
    workspace="$2"
    path="$3"
    # Do something after workspace creation
    ;;
esac
```

### Pattern 2: Workspace Query

Read state.json to find workspaces:

```bash
# In plugin
state_file="${GROVE_STATE}"
jq '.workspaces[] | select(.name == "active-workspace")' "$state_file"
```

### Pattern 3: Tool Integration

Pass workspace info to external tools:

```bash
# gw-notify — Send workspace notifications
workspace="${GROVE_WORKSPACE}"
[ -n "$workspace" ] && notify "Entered workspace: $workspace"
```

### Pattern 4: Custom Config

Store plugin config in `~/.grove/config.toml`:

```toml
[plugins.mycommand]
api_key = "..."
debug = true
```

Parse it as TOML in your plugin.

---

## Best Practices

### For Plugin Authors

1. **Use environment variables** — Don't assume state.json location or config format
2. **Fail gracefully** — Missing config should be a warning, not an error
3. **Document placeholders** — Show users which `{placeholder}` your hooks support
4. **Test with worktrees** — Verify your plugin works with actual git worktrees
5. **Avoid heavy dependencies** — Single static binary preferred (like Grove itself)

### For Plugin Users

1. **Keep hooks fast** — Long-running hooks block workspace creation/deletion
2. **Use stream for long tasks** — Set `stream = true` for progress visibility
3. **Set timeouts** — Prevent hung hooks from blocking your terminal
4. **Test locally first** — Try a hook manually before adding to config
5. **Monitor logs** — Use `--verbose` to debug hook failures

---

## Troubleshooting Integration Issues

### Plugin Not Found

```bash
gw myplugin
# → zsh: command not found: gw-myplugin
```

Ensure `gw-myplugin` is installed:
```bash
which gw-myplugin  # Should be in PATH or ~/.grove/plugins/
chmod +x ~/.grove/plugins/gw-myplugin
```

### Hook Not Firing

Check config syntax:
```bash
gw doctor
gw --verbose create my-feature  # Show hook firing
```

Verify hook is in `~/.grove/config.toml`:
```bash
grep "post_create" ~/.grove/config.toml
```

### Memory Sync Issues

Ensure `gw-claude` plugin is installed:
```bash
gw plugin list | grep claude
gw plugin install nicksenap/gw-claude
```

Verify hooks are configured:
```bash
grep "gw claude" ~/.grove/config.toml
```

Check memory directory:
```bash
ls -la ~/.grove/.claude-memory/
```

---

## Next Steps

- Learn how to [configure hooks](operations.md#lifecycle-hooks) for your plugins
- Explore [plugin source code](https://github.com/nicksenap/gw-claude) as templates
- Join the Grove community to share and discover plugins
