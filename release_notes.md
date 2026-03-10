## Agent Dashboard (`gw dash`)

New Textual TUI for monitoring Claude Code agents across all your workspaces in real-time.

### Features

- **Live agent monitoring** — polls `~/.grove/status/` every 500ms to show all active Claude Code agents with status, tools, activity sparklines, and error counts
- **Zellij integration** — press `Enter` to jump to an agent's terminal tab using smart 5-level tab matching (exact name, workspace name, CWD path, substring)
- **Permission management** — approve (`y`) or deny (`n`) permission requests directly from the dashboard
- **Vim-style search** — press `/` to filter agents by name, branch, tool, or status
- **Grove workspace awareness** — resolves agent CWDs to Grove workspaces, showing workspace name and repo list in the detail panel
- **Gruvbox Dark theme** — consistent styling with proper Textual theme integration

### Agent data collected via hooks

- Status (idle, working, waiting permission, waiting answer, error)
- Git branch and dirty file count
- Tool call count, error count, subagent count and types
- Activity sparkline, compaction count and trigger type
- Permission mode, session source (startup/resume/compact)
- Initial prompt, last agent response, last error message
- Permission request details with diff-style summaries

### Setup

```bash
gw dash install   # install Claude Code hooks
gw dash           # launch dashboard
gw dash uninstall # remove hooks
gw dash status    # check hook installation
gw dash list      # list active agents (non-TUI)
```

## Upgrading

```bash
brew upgrade grove
```
