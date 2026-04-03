# Dashboard (`gw dash`)

A Textual TUI for monitoring Claude Code agents across all your workspaces. Agents are sorted into a kanban board with four columns — **Active**, **Attention**, **Idle**, **Done** — based on live status. Inspired by [Clorch](https://github.com/androsovm/clorch).

```bash
gw dash install   # install Claude Code hooks
gw dash           # launch the dashboard
gw dash uninstall # remove hooks
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

Claude Code hooks write agent state to `~/.grove/status/<session_id>.json` on every event. The dashboard polls these files every 500ms and renders a real-time kanban view of all active agents.

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

Grove integrates with [Zellij](https://zellij.dev/) for tab management. When running inside Zellij:

- **`gw go --close-tab`** closes the current Zellij tab (useful when you're done with a workspace)
- **`gw dash`** lets you press `Enter` to jump directly to an agent's Zellij tab, and `y`/`n` to approve or deny permission requests

Support for tmux and other terminal multiplexers is planned.
