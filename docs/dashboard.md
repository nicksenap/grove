# Dashboard (`gw dash`)

A kanban-style TUI for monitoring Claude Code agents across all your workspaces. Agents are sorted into four columns — **Active**, **Attention**, **Idle**, **Done** — based on live status. Inspired by [Clorch](https://github.com/androsovm/clorch).

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lipgloss](https://github.com/charmbracelet/lipgloss).

## Install

```bash
gw plugin install nicksenap/gw-dash     # install the dashboard plugin
gw claude hook install                   # register session tracking hooks (requires gw-claude)
gw dash                                  # launch
```

| Key | Action |
|-----|--------|
| `h` / `l` | Navigate columns |
| `j` / `k` | Navigate cards |
| `Enter` | Jump to agent's Zellij tab |
| `y` / `n` | Approve / deny permission request |
| `/` | Search / filter agents |
| `Escape` | Clear search |
| `r` | Refresh |
| `q` | Quit |

## How it works

The [gw-claude](https://github.com/nicksenap/gw-claude) plugin's hook handler writes agent state to `~/.grove/status/<session_id>.json` on every Claude Code event. The dashboard polls these files every 500ms and renders a real-time kanban view.

**Tracked per agent:** status, working directory, git branch, dirty file count, last tool used, tool/error/subagent counts, activity sparkline, permission request details, and initial prompt.

## Zellij tab matching

When you press `Enter` to jump to an agent, the dashboard finds the right Zellij tab using a multi-step strategy:

| Priority | Strategy | Example |
|----------|----------|---------|
| 1 | Exact tab name = project name | `grove` → tab `grove` |
| 2 | Case-insensitive tab name | `Grove` → tab `grove` |
| 3 | Workspace name from CWD matched against tab names | CWD has `feat-rewrite` → tab matching |
| 4 | CWD path match via `zellij action dump-layout` | Agent CWD under tab CWD or vice versa |
| 5 | Project name substring in tab name | `api` → tab `public-api` |

## Terminal multiplexer integration

The dashboard integrates with [Zellij](https://zellij.dev/) for tab management. When running inside Zellij:

- **`Enter`** jumps directly to an agent's Zellij tab
- **`y` / `n`** approves or denies permission requests remotely

For closing panes when navigating away from workspaces, use the [gw-zellij](https://github.com/nicksenap/gw-zellij) plugin:

```bash
gw plugin install nicksenap/gw-zellij
```

```toml
[hooks]
on_close = "gw zellij close-pane"
```
